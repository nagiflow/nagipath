package collect

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"fmt"
	"net"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/parse"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
)

const (
	maxFileBytes  = 2 << 20 // 2 MiB per configuration file
	maxTotalBytes = 64 << 20
)

// Host is one connected managed host: the command surface plus the two facts
// Check learned about it.
type Host interface {
	sshx.Executor
	Facts() (osFamily string, sudo bool)
}

// Connector opens a Host for a Node. The seam exists so the collector can be
// tested against recorded vendor output instead of a live fleet.
type Connector interface {
	Connect(ctx context.Context, nodeID int64) (Host, error)
}

// SSH adapts the real dialer to Connector.
type SSH struct{ Dialer *sshx.Dialer }

func (s SSH) Connect(ctx context.Context, nodeID int64) (Host, error) {
	return s.Dialer.Connect(ctx, nodeID)
}

type Collector struct {
	DB     *store.DB
	Dialer Connector
}

// Node collects one Node end to end. It always closes the Collection row, even on
// failure: a Collection stuck in `running` is indistinguishable from a hung
// process, and an operator cannot tell those apart from the UI.
func (c *Collector) Node(ctx context.Context, nodeID int64, trigger string, actor *int64) (err error) {
	start := time.Now()
	collectionID, err := c.DB.StartCollection(ctx, nodeID, trigger, actor)
	if err != nil {
		return err
	}
	var (
		instances int
		bytes     int64
		degraded  []string
	)
	defer func() {
		status, msg := "succeeded", ""
		switch {
		case err != nil:
			status, msg = "failed", err.Error()
		case len(degraded) > 0:
			status, msg = "degraded", strings.Join(degraded, "; ")
		}
		c.DB.FinishCollection(ctx, collectionID, status, msg, instances, bytes,
			time.Since(start).Milliseconds())
		c.DB.NodeFailure(ctx, nodeID, status == "failed")
	}()

	cl, err := c.Dialer.Connect(ctx, nodeID)
	if err != nil {
		return err
	}
	defer cl.Close()

	if err = cl.Check(ctx); err != nil {
		return err
	}
	osFamily, sudo := cl.Facts()
	if err = c.DB.SetNodeFacts(ctx, nodeID, osFamily, sudo); err != nil {
		return err
	}

	found, err := Discover(ctx, cl)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		degraded = append(degraded, "no nginx, Apache or HAProxy found on this host")
		return nil
	}

	serviceManager := "unknown"
	if res, e := cl.Run(ctx, sshx.SystemdUnits()); e == nil && strings.TrimSpace(res.Stdout) != "" {
		serviceManager = "systemd"
	} else if hasPID1(found) {
		serviceManager = "container"
	}

	var seen []int64
	for _, f := range found {
		capd := c.capture(ctx, cl, f)
		if len(capd.files) == 0 {
			degraded = append(degraded,
				fmt.Sprintf("%s: no configuration could be read (%s)", f.Vendor, capd.reason))
			continue
		}

		instanceID, e := c.DB.UpsertInstance(ctx, store.Instance{
			NodeID:         nodeID,
			Vendor:         f.Vendor,
			NaturalKey:     f.NaturalKey(),
			DisplayName:    f.Vendor + " " + path.Base(capd.mainConfig),
			Version:        capd.version,
			BinaryPath:     f.Binary,
			ConfigRoot:     path.Dir(capd.mainConfig),
			MainConfigPath: capd.mainConfig,
			BuildFlags:     capd.buildFlags,
			ServiceManager: serviceManager,
			UnitName:       f.Unit,
			DetectedPID:    nullPID(f.PID),
		})
		if e != nil {
			return e
		}
		seen = append(seen, instanceID)
		instances++

		snapshotID, e := c.DB.WriteSnapshot(ctx, store.Snapshot{
			InstanceID:     instanceID,
			CollectionID:   collectionID,
			ConfigSource:   capd.source,
			Degraded:       capd.degraded,
			DegradedReason: capd.reason,
		}, capd.files)
		if e != nil {
			return e
		}
		for _, file := range capd.files {
			bytes += int64(len(file.Content))
		}
		if capd.degraded {
			degraded = append(degraded, f.Vendor+": "+capd.reason)
		}

		res := c.parse(f.Vendor, capd)
		if res.Degraded {
			degraded = append(degraded, f.Vendor+": "+res.DegradedReason)
		}
		if e := c.DB.SaveDerived(ctx, snapshotID, instanceID, res); e != nil {
			c.DB.SetParseState(ctx, snapshotID, "failed", e.Error())
			degraded = append(degraded, f.Vendor+": persisting the parse failed: "+e.Error())
			continue
		}
		if e := c.DB.SetInstanceLogPaths(ctx, instanceID, logPaths(res)); e != nil {
			return e
		}
		c.resolveNames(ctx, cl, nodeID, res)
		c.certificates(ctx, cl, instanceID, snapshotID, res)
	}
	return c.DB.RetireMissingInstances(ctx, nodeID, seen)
}

type capture struct {
	files      []store.SnapshotFile
	source     string
	degraded   bool
	reason     string
	version    string
	buildFlags string
	mainConfig string
}

func (cp *capture) fail(reason string) {
	cp.degraded = true
	if cp.reason == "" {
		cp.reason = reason
	} else {
		cp.reason += "; " + reason
	}
}

func (c *Collector) capture(ctx context.Context, ex sshx.Executor, f Found) *capture {
	cp := &capture{source: "vendor_dump"}
	switch f.Vendor {
	case "nginx":
		c.captureNginx(ctx, ex, f, cp)
	case "apache":
		c.captureApache(ctx, ex, f, cp)
	case "haproxy":
		c.captureHAProxy(ctx, ex, f, cp)
	}
	return cp
}

// ------------------------------------------------------------------- nginx

func (c *Collector) captureNginx(ctx context.Context, ex sshx.Executor, f Found, cp *capture) {
	bin := f.Binary
	if bin == "" {
		bin = "nginx"
	}
	if res, err := ex.Run(ctx, sshx.NginxVersion(bin)); err == nil {
		out := merged(res)
		cp.version = afterColon(grepLine(out, "nginx version:"))
		cp.buildFlags = afterColon(grepLine(out, "configure arguments:"))
		if p := flagArg(cp.buildFlags, "--conf-path="); p != "" {
			cp.mainConfig = p
		}
	}
	if len(f.ConfigPaths) > 0 {
		cp.mainConfig = f.ConfigPaths[0]
	}
	if cp.mainConfig == "" {
		cp.mainConfig = "/etc/nginx/nginx.conf"
	}

	// `nginx -T` is the authority (ADR-0004): it prints the complete effective
	// configuration, so an include we could not have guessed still arrives.
	res, err := ex.Run(ctx, sshx.NginxDump(bin, ""))
	if err == nil {
		dump := dumpStream(res)
		if files := splitNginxDump(dump); len(files) > 0 {
			cp.files = append(cp.files, files...)
			cp.files = append(cp.files, store.SnapshotFile{
				Kind: "vendor_dump", Path: bin + " -T", Content: []byte(dump),
			})
			return
		}
		cp.fail("nginx -T produced no configuration; falling back to reading files")
	} else {
		cp.fail("nginx -T failed: " + err.Error())
	}
	cp.source = "fallback_walk"
	cp.files = append(cp.files, c.walk(ctx, ex, cp, cp.mainConfig, path.Dir(cp.mainConfig))...)
}

// merged is both streams, because the vendors disagree about which one they use:
// nginx prints -V to stderr while httpd and haproxy print their version to
// stdout. The commands themselves carry no `2>&1` — a redirection would make
// them ineligible for sudo.
func merged(res sshx.Result) string { return res.Stdout + res.Stderr }

// dumpStream picks the stream the dump actually landed on, rather than
// concatenating: the banners delimit file contents byte for byte, so appending
// the other stream would splice diagnostics into the last file's body and make
// its provenance offsets lie.
func dumpStream(res sshx.Result) string {
	if strings.Contains(res.Stdout, nginxDumpBanner) {
		return res.Stdout
	}
	if strings.Contains(res.Stderr, nginxDumpBanner) {
		return res.Stderr
	}
	return res.Stdout
}

const nginxDumpBanner = "# configuration file "

// splitNginxDump cuts the dump on its `# configuration file <path>:` banners.
// Everything between two banners is one file, byte for byte, which is what makes
// the parser's provenance offsets meaningful.
func splitNginxDump(dump string) []store.SnapshotFile {
	const banner = nginxDumpBanner
	var out []store.SnapshotFile
	var cur *store.SnapshotFile
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Content = []byte(body.String())
			out = append(out, *cur)
		}
		body.Reset()
	}
	for _, line := range strings.SplitAfter(dump, "\n") {
		trimmed := strings.TrimRight(line, "\r\n")
		if p, ok := strings.CutPrefix(trimmed, banner); ok && strings.HasSuffix(p, ":") {
			flush()
			cur = &store.SnapshotFile{Kind: "config_file", Path: strings.TrimSuffix(p, ":")}
			continue
		}
		if cur != nil {
			body.WriteString(line)
		}
	}
	flush()
	return out
}

// ------------------------------------------------------------------ apache

func (c *Collector) captureApache(ctx context.Context, ex sshx.Executor, f Found, cp *capture) {
	bin := f.Binary
	if bin == "" {
		bin = "httpd"
	}
	root := ""
	if res, err := ex.Run(ctx, sshx.HTTPDVersion(bin)); err == nil {
		out := merged(res)
		cp.version = afterColon(grepLine(out, "Server version:"))
		cp.buildFlags = strings.TrimSpace(out)
		root = defineValue(out, "HTTPD_ROOT")
		if conf := defineValue(out, "SERVER_CONFIG_FILE"); conf != "" {
			if path.IsAbs(conf) || root == "" {
				cp.mainConfig = conf
			} else {
				cp.mainConfig = path.Join(root, conf)
			}
		}
	}
	if len(f.ConfigPaths) > 0 {
		cp.mainConfig = f.ConfigPaths[0]
	}
	if cp.mainConfig == "" {
		cp.mainConfig = "/etc/httpd/conf/httpd.conf"
	}
	if root == "" {
		root = path.Dir(path.Dir(cp.mainConfig))
	}

	// Apache has no `-T`. `-D DUMP_INCLUDES` is the closest thing: it names every
	// file the server actually reads, which is the file set we then read ourselves.
	var paths []string
	if res, err := ex.Run(ctx, sshx.HTTPDIncludes(bin, cp.mainConfig)); err == nil {
		out := merged(res)
		paths = includedPaths(out, root)
		cp.files = append(cp.files, store.SnapshotFile{
			Kind: "vendor_dump", Path: bin + " -D DUMP_INCLUDES", Content: []byte(out),
		})
	}
	for _, id := range []struct {
		label string
		cmd   sshx.Command
	}{
		{"DUMP_VHOSTS", sshx.HTTPDVhosts(bin, cp.mainConfig)},
		{"DUMP_MODULES", sshx.HTTPDModules(bin, cp.mainConfig)},
	} {
		if res, err := ex.Run(ctx, id.cmd); err == nil {
			cp.files = append(cp.files, store.SnapshotFile{
				Kind: "vendor_dump", Path: bin + " -D " + id.label, Content: []byte(merged(res)),
			})
		}
	}

	if len(paths) == 0 {
		cp.fail("DUMP_INCLUDES named no files; falling back to reading the config tree")
		cp.source = "fallback_walk"
		cp.files = append(cp.files, c.walk(ctx, ex, cp, cp.mainConfig, path.Dir(cp.mainConfig))...)
		return
	}
	cp.files = append(cp.files, c.read(ctx, ex, cp, paths)...)
}

// includedPaths reads DUMP_INCLUDES output:
//
//	Included configuration files:
//	  (*) /usr/local/apache2/conf/httpd.conf
//	    (146) /usr/local/apache2/conf/extra/httpd-vhosts.conf
func includedPaths(out, root string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "(") {
			continue
		}
		_, rest, ok := strings.Cut(line, ")")
		if !ok {
			continue
		}
		p := strings.TrimSpace(rest)
		if p == "" {
			continue
		}
		if !path.IsAbs(p) && root != "" {
			p = path.Join(root, p)
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

func defineValue(out, name string) string {
	line := grepLine(out, name+"=")
	_, rest, ok := strings.Cut(line, name+"=")
	if !ok {
		return ""
	}
	return strings.Trim(strings.TrimSpace(rest), `"`)
}

// ----------------------------------------------------------------- haproxy

func (c *Collector) captureHAProxy(ctx context.Context, ex sshx.Executor, f Found, cp *capture) {
	bin := f.Binary
	if bin == "" {
		bin = "haproxy"
	}
	if res, err := ex.Run(ctx, sshx.HAProxyVersion(bin)); err == nil {
		out := merged(res)
		cp.version = haproxyVersion(out)
		cp.buildFlags = strings.TrimSpace(out)
	}
	paths := f.ConfigPaths
	if len(paths) == 0 {
		// No -f on the command line means haproxy is not running from an explicit
		// config; the conventional locations are the only remaining evidence.
		paths = []string{"/etc/haproxy/haproxy.cfg", "/usr/local/etc/haproxy/haproxy.cfg"}
	}
	if len(paths) > 0 {
		cp.mainConfig = paths[0]
	}
	// HAProxy has no include directive, so the -f set is provably the complete
	// configuration. That makes this the one vendor where "did we get everything?"
	// has a certain answer.
	cp.files = append(cp.files, c.read(ctx, ex, cp, paths)...)
	if len(cp.files) > 0 {
		cp.mainConfig = cp.files[0].Path
		if res, err := ex.Run(ctx, sshx.HAProxyCheck(bin, cp.mainConfig)); err == nil {
			cp.files = append(cp.files, store.SnapshotFile{
				Kind: "vendor_dump", Path: bin + " -c", Content: []byte(merged(res)),
			})
		}
	}
}

func haproxyVersion(out string) string {
	line := grepLine(out, "version")
	for _, prefix := range []string{"HAProxy version ", "HA-Proxy version "} {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.Fields(v)[0]
		}
	}
	return strings.TrimSpace(line)
}

// -------------------------------------------------------------- file access

func (c *Collector) read(ctx context.Context, ex sshx.Executor, cp *capture, paths []string) []store.SnapshotFile {
	var out []store.SnapshotFile
	var total int64
	for _, p := range paths {
		if total > maxTotalBytes {
			cp.fail("configuration exceeds the size cap; the rest was not read")
			break
		}
		data, truncated, err := ex.ReadFile(ctx, p, maxFileBytes, false)
		if err != nil {
			cp.fail("could not read " + p + ": " + err.Error())
			continue
		}
		if truncated {
			cp.fail(p + " was truncated at the size cap")
		}
		total += int64(len(data))
		out = append(out, store.SnapshotFile{
			Kind: "config_file", Path: p, Content: data, Truncated: truncated,
		})
	}
	return out
}

// walk is the last resort: read the main config, then everything conf-shaped under
// its directory. It cannot prove completeness, which is exactly why the Snapshot
// is marked fallback_walk and shown as degraded.
func (c *Collector) walk(ctx context.Context, ex sshx.Executor, cp *capture, main, root string) []store.SnapshotFile {
	paths := []string{main}
	if res, err := ex.Run(ctx, sshx.FindConf(root, false)); err == nil {
		for _, line := range strings.Split(res.Stdout, "\n") {
			p := strings.TrimSpace(line)
			if p != "" && p != main {
				paths = append(paths, p)
			}
			if len(paths) >= sshx.FindConfMax {
				cp.fail(fmt.Sprintf("stopped the fallback walk of %s at %d files", root, sshx.FindConfMax))
				break
			}
		}
	}
	sort.Strings(paths[1:])
	return c.read(ctx, ex, cp, paths)
}

// ------------------------------------------------------------------- parse

func (c *Collector) parse(vendor string, cp *capture) *parse.Result {
	files := make([]parse.File, 0, len(cp.files))
	for _, f := range cp.files {
		if f.Kind == "config_file" {
			files = append(files, parse.File{Path: f.Path, Content: f.Content})
		}
	}
	switch vendor {
	case "nginx":
		return parse.NGINX(files, cp.mainConfig)
	case "apache":
		return parse.Apache(files, cp.mainConfig)
	default:
		return parse.HAProxy(files)
	}
}

// isProcFD reports whether p is a /proc file-descriptor path — /proc/self/fd/1,
// /proc/1/fd/2 and friends — which resolve to a pipe or socket, not a file with
// content to tail.
func isProcFD(p string) bool {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if part == "fd" && i > 0 && (parts[i-1] == "self" || isDigits(parts[i-1])) {
			return true
		}
	}
	return false
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func logPaths(res *parse.Result) []string {
	seen := map[string]bool{}
	var out []string
	visit := func(rules []*parse.Rule) {
		for _, r := range rules {
			switch strings.ToLower(r.Directive) {
			case "access_log", "customlog", "transferlog", "log":
			default:
				continue
			}
			p := strings.Fields(r.Args)
			// /dev/log and friends are the syslog socket HAProxy's `log` directive
			// names, not a file nagipath can tail. /proc/self/fd/* and /proc/*/fd/*
			// are the same class of non-file: the common container convention of
			// pointing access_log/CustomLog at stdout/stderr by fd, which `tail`
			// can open but which never accumulates anything to read back.
			if len(p) == 0 || p[0] == "off" || !strings.HasPrefix(p[0], "/") ||
				strings.HasPrefix(p[0], "/dev/") || isProcFD(p[0]) {
				continue
			}
			if !seen[p[0]] {
				seen[p[0]] = true
				out = append(out, p[0])
			}
		}
	}
	visit(res.GlobalRules)
	for _, s := range res.Sites {
		visit(s.Rules)
		walkRoutes(s.Routes, func(r *parse.Route) { visit(r.Rules) })
	}
	// HAProxy's own default is syslog (`log stdout ...` or a socket), never a file
	// directive the parser can see. Most distributions still land that stream in one
	// of these two conventional locations via the default rsyslog rule for
	// local0/local1 — Debian/Ubuntu's haproxy package uses the first, RHEL-family
	// packaging the second. The last two are the shared syslog itself, tried only
	// if neither dedicated file exists — a host with no local0/local1 split still
	// has haproxy's lines in there, just interleaved with every other daemon's.
	// ponytail: logTailBytes is a fixed window, so a noisy shared syslog can push
	// the matching line out of it before the token search ever sees it; move to a
	// grep-for-token-then-tail-context read if that starts happening in practice.
	if len(out) == 0 && res.Vendor == "haproxy" {
		out = append(out, "/var/log/haproxy.log", "/var/log/haproxy/haproxy.log",
			"/var/log/syslog", "/var/log/messages")
	}
	return out
}

func walkRoutes(routes []*parse.Route, fn func(*parse.Route)) {
	for _, r := range routes {
		fn(r)
		walkRoutes(r.Routes, fn)
	}
}

// ------------------------------------------------------------------- extras

// resolveNames resolves upstream member hostnames **on the target**, because in a
// fleet the control plane's view of DNS is routinely not the server's view.
func (c *Collector) resolveNames(ctx context.Context, ex sshx.Executor, nodeID int64, res *parse.Result) {
	seen := map[string]bool{}
	for _, up := range res.Upstreams {
		for _, m := range up.Members {
			name := m.Host
			if name == "" || seen[name] || net.ParseIP(name) != nil {
				continue
			}
			seen[name] = true
			out, err := ex.Run(ctx, sshx.GetentHosts(name))
			if err != nil {
				continue
			}
			addrs, method := parseGetent(out.Stdout)
			c.DB.SaveDNS(ctx, nodeID, name, addrs, method)
		}
	}
}

func parseGetent(out string) ([]string, string) {
	var addrs []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && net.ParseIP(fields[0]) != nil {
			addrs = append(addrs, fields[0])
		}
	}
	if len(addrs) == 0 {
		return nil, "unresolved"
	}
	return addrs, "getent_hosts"
}

// certificates extracts certificate metadata on the target with `openssl x509
// -noout`. The key file is never read and never transferred; where openssl is
// missing, the binding is recorded without metadata rather than guessed at.
func (c *Collector) certificates(ctx context.Context, ex sshx.Executor, instanceID, snapshotID int64, res *parse.Result) {
	if len(res.CertBindings) == 0 {
		return
	}
	siteIDs, err := c.DB.SiteIDsByKey(ctx, snapshotID)
	if err != nil {
		return
	}
	files, err := c.DB.SnapshotFiles(ctx, snapshotID)
	if err != nil {
		return
	}
	fileID := map[string]int64{}
	for _, f := range files {
		fileID[f.Path] = f.ID
	}

	for _, b := range res.CertBindings {
		out, err := ex.Run(ctx, sshx.X509(b.CertPath, false))
		if err != nil || out.ExitCode != 0 {
			continue
		}
		cert, ok := parseX509(out.Stdout)
		if !ok {
			continue
		}
		certID, err := c.DB.UpsertCertificate(ctx, cert)
		if err != nil {
			continue
		}
		row := store.CertBindingRow{
			CertificateID: certID,
			InstanceID:    instanceID,
			SnapshotID:    snapshotID,
			FilePath:      b.CertPath,
			CombinedPEM:   b.CombinedPEM,
		}
		if id, ok := siteIDs[b.SiteKey]; ok {
			row.SiteID = &id
		}
		if id, ok := fileID[b.Path]; ok {
			s, e := b.Start, b.End
			row.ProvFileID, row.ProvStart, row.ProvEnd = &id, &s, &e
		}
		c.DB.AddCertBinding(ctx, row)
	}
}

// parseX509 reads the fixed field set that sshx.X509 asks for. Anything it cannot
// find stays empty; a certificate without a fingerprint is not stored at all,
// since the fingerprint is its identity.
func parseX509(out string) (store.Certificate, bool) {
	c := store.Certificate{}
	var pubkey []string
	inPubkey := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// The -pubkey PEM block is collected whole and parsed below rather than
		// scanned line by line, because base64 means nothing on its own.
		if strings.HasPrefix(line, "-----BEGIN PUBLIC KEY") {
			inPubkey = true
		}
		if inPubkey {
			pubkey = append(pubkey, line)
			if strings.HasPrefix(line, "-----END PUBLIC KEY") {
				inPubkey = false
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "subject="):
			c.SubjectDN = strings.TrimSpace(strings.TrimPrefix(line, "subject="))
			c.SubjectCN = rdn(c.SubjectDN, "CN")
		case strings.HasPrefix(line, "issuer="):
			c.IssuerDN = strings.TrimSpace(strings.TrimPrefix(line, "issuer="))
		case strings.HasPrefix(line, "serial="):
			c.Serial = strings.TrimSpace(strings.TrimPrefix(line, "serial="))
		case strings.HasPrefix(line, "notBefore="):
			c.NotBefore = asnTime(strings.TrimPrefix(line, "notBefore="))
		case strings.HasPrefix(line, "notAfter="):
			c.NotAfter = asnTime(strings.TrimPrefix(line, "notAfter="))
		case strings.HasPrefix(line, "sha256 Fingerprint="), strings.HasPrefix(line, "SHA256 Fingerprint="):
			_, v, _ := strings.Cut(line, "=")
			c.Fingerprint = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), ":", ""))
		case strings.HasPrefix(line, "DNS:"), strings.HasPrefix(line, "IP Address:"):
			for _, san := range strings.Split(line, ",") {
				san = strings.TrimSpace(san)
				if _, v, ok := strings.Cut(san, ":"); ok && v != "" {
					c.SANs = append(c.SANs, strings.TrimSpace(v))
				}
			}
		}
	}
	if c.Fingerprint == "" || c.NotAfter == "" {
		return c, false
	}
	c.KeyAlgorithm, c.KeyBits = publicKeyStrength(strings.Join(pubkey, "\n"))
	c.SelfSigned = c.SubjectDN != "" && c.SubjectDN == c.IssuerDN
	return c, true
}

// publicKeyStrength names the key an operator has to judge: "RSA" with its
// modulus size, or "ECDSA"/"Ed25519" with the curve's. An openssl too old for
// -pubkey, or a key type crypto/x509 does not know, leaves both empty — the
// certificates list distinguishes "not collected" from a weak key, so guessing
// here would be worse than the blank it replaces.
func publicKeyStrength(pemText string) (string, sql.NullInt64) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return "", sql.NullInt64{}
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", sql.NullInt64{}
	}
	bits := func(n int) sql.NullInt64 { return sql.NullInt64{Int64: int64(n), Valid: true} }
	switch k := key.(type) {
	case *rsa.PublicKey:
		return "RSA", bits(k.N.BitLen())
	case *ecdsa.PublicKey:
		return "ECDSA", bits(k.Curve.Params().BitSize)
	case ed25519.PublicKey:
		return "Ed25519", bits(len(k) * 8)
	}
	return "", sql.NullInt64{}
}

// asnTime converts openssl's `Jan  2 15:04:05 2006 GMT` to the store's sortable
// UTC format. An unparseable date is kept verbatim rather than zeroed, so an
// expiry view can still show something true.
func asnTime(s string) string {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"Jan _2 15:04:05 2006 MST", "Jan 2 15:04:05 2006 MST"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	return s
}

func rdn(dn, key string) string {
	for _, part := range strings.FieldsFunc(dn, func(r rune) bool { return r == ',' || r == '/' }) {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), key) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ------------------------------------------------------------------- helpers

func grepLine(out, needle string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func afterColon(line string) string {
	_, rest, ok := strings.Cut(line, ":")
	if !ok {
		return strings.TrimSpace(line)
	}
	return strings.TrimSpace(rest)
}

func flagArg(flags, prefix string) string {
	for _, f := range strings.Fields(flags) {
		if v, ok := strings.CutPrefix(f, prefix); ok {
			return v
		}
	}
	return ""
}

func hasPID1(found []Found) bool {
	for _, f := range found {
		if f.PID == 1 {
			return true
		}
	}
	return false
}

func nullPID(pid int) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(pid), Valid: pid > 0}
}
