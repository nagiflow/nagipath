// Command nagipath is the whole product: control plane, web UI and CLI in one
// binary, with no runtime dependency beyond a writable directory for its database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/keys"
	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
	"github.com/nagiflow/nagipath/internal/web"
)

// Version is stamped at build time with -ldflags "-X main.Version=...".
var Version = "dev"

const usage = `nagipath — vendor-neutral web server fleet inventory and request tracing

Usage:
  nagipath server    [flags]      run the control plane and web UI
  nagipath migrate   [flags]      apply schema migrations and exit
  nagipath keygen    [flags]      create the master key file
  nagipath backup    [flags]      write a consistent copy of the database
  nagipath bootstrap [flags]      create the first admin, a credential and a host list
  nagipath collect   [flags] NODE collect one node by id or address
  nagipath trace     [flags] URL  trace a request and print the result
  nagipath version                print the version

Common flags:
  -data DIR    state directory (default ~/.nagipath, or $NAGIPATH_DATA)

The master key is read from $NAGIPATH_MASTER_KEY, or from <data>/master.key.
The server refuses to start without it rather than generating a new one: a new key
would silently make every stored credential undecryptable.

Setting $NAGIPATH_DEMO_MODE to any non-empty value refuses every Probe outright — for
a public-facing trial instance that must never send a real outbound request.

TLS: set both $NAGIPATH_TLS_CERT and $NAGIPATH_TLS_KEY (or -tls-cert/-tls-key) for
direct TLS. Without them the server speaks plain HTTP — fine behind a customer's own
TLS-terminating reverse proxy, otherwise pass -secure-cookies only once that proxy
is actually terminating TLS in front of it.

Metrics: GET /metrics is 404 until $NAGIPATH_METRICS_TOKEN (or -metrics-token) is
set, then it requires "Authorization: Bearer <token>" and serves Prometheus text
exposition format. GET /readyz and GET /healthz are always unauthenticated.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "nagipath: "+err.Error())
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "server":
		return cmdServer(args)
	case "migrate":
		return cmdMigrate(args)
	case "keygen":
		return cmdKeygen(args)
	case "backup":
		return cmdBackup(args)
	case "bootstrap":
		return cmdBootstrap(args)
	case "collect":
		return cmdCollect(args)
	case "trace":
		return cmdTrace(args)
	case "version", "-v", "--version":
		fmt.Println("nagipath " + Version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	fmt.Fprint(os.Stderr, usage)
	return fmt.Errorf("unknown command %q", cmd)
}

// dataDir resolves the state directory. Order: flag, environment, then a
// per-user default, so a bare `nagipath server` works with no setup at all.
func dataDir(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if env := os.Getenv("NAGIPATH_DATA"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".nagipath"), nil
}

func openDB(dir string) (*store.DB, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(dir, "nagipath.db"))
}

func loadMaster(dir string) (*keys.Master, error) {
	m, err := keys.Load(os.Getenv("NAGIPATH_MASTER_KEY"), filepath.Join(dir, "master.key"))
	if err != nil {
		return nil, fmt.Errorf("%w\nrun `nagipath keygen` to create one, or set NAGIPATH_MASTER_KEY", err)
	}
	return m, nil
}

// logger builds the process logger. extra, when non-nil, receives a copy of
// every line alongside stderr — cmdServer passes its diagnostics ring buffer;
// every other subcommand passes nil and logs to stderr only.
func logger(format string, extra io.Writer) *slog.Logger {
	out := io.Writer(os.Stderr)
	if extra != nil {
		out = io.MultiWriter(os.Stderr, extra)
	}
	if format == "json" {
		return slog.New(slog.NewJSONHandler(out, nil))
	}
	return slog.New(slog.NewTextHandler(out, nil))
}

// ---------------------------------------------------------------- server

func cmdServer(args []string) error {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	addr := fs.String("listen", envOr("NAGIPATH_LISTEN", "127.0.0.1:8080"), "listen address")
	logFormat := fs.String("log", "text", "log format: text or json")
	secure := fs.Bool("secure-cookies", false, "mark session cookies Secure (requires HTTPS)")
	tlsCert := fs.String("tls-cert", os.Getenv("NAGIPATH_TLS_CERT"), "TLS certificate file (enables direct TLS)")
	tlsKey := fs.String("tls-key", os.Getenv("NAGIPATH_TLS_KEY"), "TLS private key file (enables direct TLS)")
	interval := fs.Duration("collect-every", 0, "collect every node on this interval (0 disables)")
	licensePath := fs.String("license", os.Getenv("NAGIPATH_LICENSE_FILE"), "license file (default <data>/license.lic)")
	metricsToken := fs.String("metrics-token", os.Getenv("NAGIPATH_METRICS_TOKEN"),
		"bearer token required by GET /metrics (unset disables the endpoint, returning 404)")
	fs.Parse(args)

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	db, err := openDB(d)
	if err != nil {
		return err
	}
	defer db.Close()
	master, err := loadMaster(d)
	if err != nil {
		return err
	}
	// The diagnostics bundle's "recent logs" section reads this back; see
	// internal/web/logbuf.go.
	logs := web.NewRingBuffer()
	log := logger(*logFormat, logs)

	// A probe on a target host is identified by its User-Agent, so the build has to
	// reach the probe rather than staying in main.
	web.Version = Version

	demoMode := os.Getenv("NAGIPATH_DEMO_MODE") != ""

	// ADR-0014: a missing or invalid license is never a startup failure — it is
	// logged and shown, and the server runs exactly as it would licensed.
	licFile := *licensePath
	if licFile == "" {
		licFile = filepath.Join(d, "license.lic")
	}
	lic, licErr := license.Load(licFile)
	if licErr != nil {
		log.Warn("no valid license loaded; nagipath continues to run unlicensed (ADR-0014)",
			"path", licFile, "err", licErr)
		lic = nil
	}

	srv, err := web.New(db, master, log, *secure, demoMode, lic, *metricsToken, licFile)
	if err != nil {
		return err
	}
	// Set directly rather than threading through New: the diagnostics panel is
	// the only consumer, and New's parameter list is long enough already.
	srv.StartedAt = time.Now()
	srv.ListenAddr = *addr
	srv.TLSEnabled = *tlsCert != "" || *tlsKey != ""
	srv.Logs = logs
	if demoMode {
		log.Info("demo mode: probes are disabled (NAGIPATH_DEMO_MODE is set)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// One audit entry per boot satisfies ADR-0014's "report entry" requirement.
	// The actor is nil: this is a system event, not something a user did.
	{
		status, _ := srv.LicenseStatus(ctx)
		if status != license.Valid {
			log.Warn("license status", "status", status, "message", status.Message())
		}
		if err := db.Audit(ctx, nil, "license.status", "license", nil, string(status)); err != nil {
			log.Warn("could not write the startup license audit entry", "err", err)
		}
	}

	// Collections are goroutines, not database-backed jobs: a 'running' row
	// left over from before this process started can only mean the previous
	// process died mid-collection. Reconcile once, here, before anything new
	// can start — never on a timer, which would misfire against a Collection
	// that is genuinely still running.
	if n, err := db.ReconcileInterruptedCollections(ctx); err != nil {
		log.Warn("could not reconcile interrupted collections", "err", err)
	} else if n > 0 {
		log.Warn("marked collections interrupted by the previous restart", "count", n)
	}

	go housekeeping(ctx, db, log)
	if *interval > 0 {
		go scheduledCollection(ctx, db, master, log, *interval)
	}

	scheme := "http"
	if *tlsCert != "" || *tlsKey != "" {
		scheme = "https"
	}
	if n, _ := db.UserCount(ctx); n == 0 {
		log.Info("no users yet — open the address below to create the first administrator",
			"url", scheme+"://"+*addr+"/setup")
	}
	return srv.Listen(ctx, *addr, *tlsCert, *tlsKey)
}

// housekeeping expires sessions and reclaims orphaned blobs. Hourly is often
// enough for both: neither is urgent, and neither should ever run on a request.
func housekeeping(ctx context.Context, db *store.DB, log *slog.Logger) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := db.ExpireSessions(ctx); err != nil {
			log.Warn("expire sessions", "err", err)
		}
		if n, err := db.GCBlobs(ctx); err != nil {
			log.Warn("blob gc", "err", err)
		} else if n > 0 {
			log.Info("reclaimed orphaned blobs", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// scheduledCollection walks nodes one at a time. Collecting a fleet in parallel
// would be faster and would also hammer a shared bastion, so it is serial by
// default and the interval is the knob.
func scheduledCollection(ctx context.Context, db *store.DB, master *keys.Master, log *slog.Logger, every time.Duration) {
	c := newCollector(db, master)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		nodes, err := db.Nodes(ctx)
		if err != nil {
			log.Warn("scheduled collection", "err", err)
			continue
		}
		for _, n := range nodes {
			if !n.Enabled || n.RetiredAt.Valid {
				continue
			}
			collectNodeGuarded(ctx, c, n, log)
		}
	}
}

// collectNodeGuarded runs one Node's scheduled Collection behind a recover. SSH
// execution and vendor-output parsing are exactly the kind of code that can panic
// on a surprising input (an unexpected slice index on malformed vendor output,
// say); one bad Node must never take the scheduled-collection goroutine — and
// every other Node's collection with it — down with it.
func collectNodeGuarded(ctx context.Context, c *collect.Collector, n store.Node, log *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("panic in scheduled collection", "node", n.DisplayName,
				"panic", r, "stack", string(debug.Stack()))
		}
	}()
	if err := c.Node(ctx, n.ID, "scheduled", nil); err != nil {
		log.Warn("scheduled collection", "node", n.DisplayName, "err", err)
	}
}

func newCollector(db *store.DB, master *keys.Master) *collect.Collector {
	return &collect.Collector{DB: db, Dialer: collect.SSH{
		Dialer: &sshx.Dialer{DB: db, Master: master, Timeout: 20 * time.Second},
	}}
}

// ---------------------------------------------------------------- migrate

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	fs.Parse(args)

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	db, err := openDB(d)
	if err != nil {
		return err
	}
	defer db.Close()
	applied, err := db.AppliedMigrations(context.Background())
	if err != nil {
		return err
	}
	for _, name := range applied {
		fmt.Println("applied", name)
	}
	fmt.Printf("%s is up to date (%d migrations)\n", filepath.Join(d, "nagipath.db"), len(applied))
	return nil
}

// ---------------------------------------------------------------- keygen

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	fs.Parse(args)

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	path := filepath.Join(d, "master.key")
	// Refusing to overwrite is the whole safety property here: a second keygen over
	// a live installation would strand every stored credential.
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite it", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := keys.Generate(path); err != nil {
		return err
	}
	fmt.Printf("wrote %s (mode 0600)\nBack this up. Without it, stored credentials cannot be decrypted.\n", path)
	return nil
}

// ---------------------------------------------------------------- backup

// cmdBackup writes a consistent point-in-time copy of the database via
// SQLite's own VACUUM INTO, rather than documenting "stop the service and cp
// the file" and hoping every customer remembers the WAL caveat. VACUUM INTO
// is safe to run against a live, in-use database — no downtime, and no risk
// of the torn-file copy a plain `cp` of a WAL-mode database can produce.
func cmdBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	out := fs.String("out", "", "destination path for the backup (required)")
	fs.Parse(args)

	if *out == "" {
		return errors.New("backup: -out is required")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite a backup", *out)
	}

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	db, err := openDB(d)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.W.ExecContext(context.Background(), "VACUUM INTO ?", *out); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	fmt.Printf("wrote %s\nThis is the database only. Back up the Master Key separately — without it, stored credentials in this backup cannot be decrypted.\n", *out)
	return nil
}

// ---------------------------------------------------------------- bootstrap

// cmdBootstrap does the three things a fresh install needs that are pure typing:
// the first admin, one credential, and a host list. It deliberately does *not*
// approve host keys — that decision stays with a person, in the UI, every time.
func cmdBootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	admin := fs.String("admin", "", "username of the first administrator")
	passwordFile := fs.String("password-file", "", "file holding the administrator's password")
	credName := fs.String("credential", "", "name for the SSH credential")
	keyFile := fs.String("key", "", "private key file to store, encrypted, as that credential")
	sshUser := fs.String("ssh-user", "nagipath", "login user for the credential and hosts")
	hostsFile := fs.String("hosts", "", "host list: one `address[:port] [name]` per line")
	fs.Parse(args)

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	db, err := openDB(d)
	if err != nil {
		return err
	}
	defer db.Close()
	master, err := loadMaster(d)
	if err != nil {
		return err
	}
	ctx := context.Background()

	var actor *int64
	if *admin != "" {
		if n, _ := db.UserCount(ctx); n > 0 {
			fmt.Println("users already exist; leaving accounts alone")
		} else {
			if *passwordFile == "" {
				return errors.New("-admin needs -password-file")
			}
			raw, err := os.ReadFile(*passwordFile)
			if err != nil {
				return err
			}
			password := strings.TrimSpace(string(raw))
			if len(password) < 12 {
				return errors.New("the administrator password must be at least 12 characters")
			}
			id, err := db.CreateUser(ctx, *admin, password, "admin", *admin, false)
			if err != nil {
				return err
			}
			actor = &id
			fmt.Printf("created administrator %q\n", *admin)
		}
	}
	if actor == nil {
		// Attribute the rest to whoever the first admin is, so the audit log has an
		// actor rather than a blank.
		if users, err := db.Users(ctx); err == nil && len(users) > 0 {
			actor = &users[0].ID
		}
	}

	// Everything below is skip-if-present, so this command can be the start command
	// of a container and run unchanged on every boot.
	existingCreds, err := db.Credentials(ctx)
	if err != nil {
		return err
	}
	var credID *int64
	if *keyFile != "" {
		name := *credName
		if name == "" {
			name = filepath.Base(*keyFile)
		}
		for i, c := range existingCreds {
			if c.Name == name {
				credID = &existingCreds[i].ID
				fmt.Printf("credential %q already exists; leaving it alone\n", name)
			}
		}
		if credID == nil {
			key, err := os.ReadFile(*keyFile)
			if err != nil {
				return err
			}
			id, err := db.CreateCredential(ctx, master, name, *sshUser, "private_key",
				string(key), "", "", actor)
			if err != nil {
				return err
			}
			credID = &id
			fmt.Printf("stored credential %q (encrypted at rest, never read back out)\n", name)
		}
	}

	if *hostsFile != "" {
		raw, err := os.ReadFile(*hostsFile)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		nodes, err := db.Nodes(ctx)
		if err != nil {
			return err
		}
		for _, n := range nodes {
			known[n.Address] = true
		}
		var added, skipped int
		for _, line := range strings.Split(string(raw), "\n") {
			if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			hostPort, name, _ := strings.Cut(line, " ")
			address, port := hostPort, 22
			if h, p, ok := strings.Cut(hostPort, ":"); ok {
				n, err := strconv.Atoi(p)
				if err != nil {
					return fmt.Errorf("port %q in %q is not a number", p, line)
				}
				address, port = h, n
			}
			// Same refusal as the UI: a range here would be a network scan.
			if strings.Contains(address, "/") {
				return fmt.Errorf("%q looks like a network; nagipath does not scan, list hosts one per line", address)
			}
			if known[address] {
				skipped++
				continue
			}
			name = strings.TrimSpace(name)
			if name == "" {
				name = address
			}
			if _, err := db.AddNode(ctx, address, port, name, *sshUser, credID, nil, "manual", actor); err != nil {
				return fmt.Errorf("add %s: %w", address, err)
			}
			added++
		}
		fmt.Printf("added %d node(s), %d already known\n", added, skipped)
	}

	fmt.Println("\nnext: run `nagipath collect all` to fetch host keys, then approve each one")
	fmt.Println("in the UI at /nodes. Nothing runs on a host until you do.")
	return nil
}

// ---------------------------------------------------------------- collect

func cmdCollect(args []string) error {
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	logFormat := fs.String("log", "text", "log format: text or json")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: nagipath collect NODE  (node id, address or name; `all` for every node)")
	}

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	db, err := openDB(d)
	if err != nil {
		return err
	}
	defer db.Close()
	master, err := loadMaster(d)
	if err != nil {
		return err
	}
	log := logger(*logFormat, nil)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	nodes, err := db.Nodes(ctx)
	if err != nil {
		return err
	}
	target := fs.Arg(0)
	var chosen []store.Node
	for _, n := range nodes {
		if target == "all" || n.Address == target || n.DisplayName == target ||
			strconv.FormatInt(n.ID, 10) == target {
			chosen = append(chosen, n)
		}
	}
	if len(chosen) == 0 {
		return fmt.Errorf("no node matches %q", target)
	}

	c := newCollector(db, master)
	var failed int
	for _, n := range chosen {
		if err := c.Node(ctx, n.ID, "manual", nil); err != nil {
			failed++
			// A pending host key is not a failure to fix here: it is a decision waiting
			// for a person, so say exactly that.
			var pending *store.ErrHostKeyPending
			if errors.As(err, &pending) {
				log.Warn("host key needs approval before anything runs on this node",
					"node", n.DisplayName, "fingerprint", pending.Fingerprint)
				continue
			}
			log.Error("collection failed", "node", n.DisplayName, "err", err)
			continue
		}
		log.Info("collected", "node", n.DisplayName)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d node(s) did not complete", failed, len(chosen))
	}
	return nil
}

// ---------------------------------------------------------------- trace

func cmdTrace(args []string) error {
	fs := flag.NewFlagSet("trace", flag.ExitOnError)
	dir := fs.String("data", "", "state directory")
	save := fs.Bool("save", false, "store the trace so it can be linked to from the UI")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: nagipath trace https://shop.example.com/api/v2/charge")
	}

	d, err := dataDir(*dir)
	if err != nil {
		return err
	}
	db, err := openDB(d)
	if err != nil {
		return err
	}
	defer db.Close()

	q, err := parseURL(fs.Arg(0))
	if err != nil {
		return err
	}
	ctx := context.Background()
	top, err := trace.Load(ctx, db)
	if err != nil {
		return err
	}
	tr := trace.Walk(top, q)
	if *save {
		if _, err := trace.Save(ctx, db, tr, nil); err != nil {
			return err
		}
	}
	printTrace(tr)
	return nil
}

func parseURL(raw string) (trace.Query, error) {
	q := trace.Query{Scheme: "https", Path: "/"}
	rest := raw
	if scheme, after, ok := strings.Cut(raw, "://"); ok {
		q.Scheme, rest = scheme, after
	}
	if q.Scheme != "http" && q.Scheme != "https" {
		return q, fmt.Errorf("scheme %q is not http or https", q.Scheme)
	}
	host := rest
	if h, p, ok := strings.Cut(rest, "/"); ok {
		host, q.Path = h, "/"+p
	}
	if h, port, ok := strings.Cut(host, ":"); ok {
		host = h
		n, err := strconv.Atoi(port)
		if err != nil {
			return q, fmt.Errorf("port %q is not a number", port)
		}
		q.Port = n
	}
	if host == "" {
		return q, errors.New("no hostname in the URL")
	}
	q.Hostname = host
	return q, nil
}

// printTrace writes the same facts the web view shows. The CLI is what an operator
// pastes into a ticket, so it is plain text and self-explanatory.
func printTrace(tr *trace.Trace) {
	fmt.Printf("%s://%s%s (port %d)\n", tr.Query.Scheme, tr.Query.Hostname, tr.Query.Path, tr.Query.Port)
	fmt.Printf("confidence: %s   stopped: %s\n\n", tr.Confidence, tr.TerminalReason)

	for _, note := range tr.Notes {
		fmt.Println("note:", note)
	}
	if len(tr.Notes) > 0 {
		fmt.Println()
	}

	for _, h := range tr.Hops {
		if h.IsExternal {
			fmt.Printf("%d. external -> %s\n     %s\n", h.Ordinal, h.ExternalTarget, h.ExternalReason)
			continue
		}
		fmt.Printf("%d. %s (%s)\n", h.Ordinal, h.Inst.DisplayName, h.Inst.NodeAddress)
		if h.Incomplete != "" {
			fmt.Printf("     INCOMPLETE snapshot: %s\n", h.Incomplete)
		}
		if h.Listener != nil {
			fmt.Printf("     listener %s:%d %s\n", orStar(h.Listener.Address), h.Listener.Port,
				tlsWord(h.Listener.TLS))
		}
		if h.Site != nil {
			fmt.Printf("     site     %s  (%s)\n", h.Site.PrimaryName, h.MatchedBy)
		}
		if h.Route != nil {
			fmt.Printf("     route    %s %s\n     why      %s\n",
				h.Route.MatchType, h.Route.Pattern, h.Precedence)
		}
		fmt.Printf("     path     %s -> %s\n", h.InboundPath, h.EffectivePath)
		for _, c := range h.PathChangedBy {
			fmt.Printf("              %s -> %s  by %s %s (%s)\n", c.From, c.To,
				c.Rule.Directive, c.Rule.Args, c.Rule.Path)
		}
		for _, n := range h.Next {
			fmt.Printf("     member   %s:%d", n.Member.Host, n.Member.Port)
			if len(n.ResolvedAddresses) > 0 {
				fmt.Printf("  resolves to %s", strings.Join(n.ResolvedAddresses, ", "))
			}
			if n.HopOrdinal >= 0 {
				fmt.Printf("  -> hop %d", n.HopOrdinal)
			}
			fmt.Println()
		}
		for _, r := range h.Shadowed {
			fmt.Printf("     DISCARDED %s %s  (%s)\n", r.Rule.Directive, r.Rule.Args, r.Rule.ShadowedBy)
		}
		if h.Terminal != "" {
			fmt.Printf("     stops    %s: %s\n", h.Terminal, h.ExternalReason)
		}
	}

	if len(tr.Undetermined) > 0 {
		fmt.Println("\nnot determined from configuration:")
		for _, b := range tr.Undetermined {
			fmt.Printf("  hop %d: %s — %s\n", b.HopOrdinal, b.Raw, b.Reason)
		}
	}
}

func orStar(s string) string {
	if s == "" {
		return "*"
	}
	return s
}

func tlsWord(tls bool) string {
	if tls {
		return "TLS"
	}
	return "plaintext"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
