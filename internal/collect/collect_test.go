package collect

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/parse"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
)

// fakeHost replays recorded vendor output. Every command the collector issues has
// to be answered here, so a new command in the collector cannot silently become
// an untested code path.
type fakeHost struct {
	out   map[sshx.ID]string
	files map[string]string
	ran   []sshx.ID
	// onStderr answers these commands on stderr instead of stdout. Real servers
	// disagree about which stream is which — nginx prints -V to stderr and some
	// builds print -T there too — so the collector may not assume stdout.
	onStderr map[sshx.ID]bool
}

func (f *fakeHost) Run(ctx context.Context, c sshx.Command) (sshx.Result, error) {
	f.ran = append(f.ran, c.ID)
	if c.ID == sshx.CmdFileRead {
		for path, body := range f.files {
			if strings.Contains(c.Line, path) {
				return sshx.Result{Stdout: body}, nil
			}
		}
		return sshx.Result{ExitCode: 1, Stderr: "No such file"}, nil
	}
	out, ok := f.out[c.ID]
	if !ok {
		return sshx.Result{ExitCode: 127, Stderr: "not found"}, nil
	}
	if f.onStderr[c.ID] {
		return sshx.Result{Stderr: out}, nil
	}
	return sshx.Result{Stdout: out}, nil
}

func (f *fakeHost) ReadFile(ctx context.Context, path string, max int64, sudo bool) ([]byte, bool, error) {
	body, ok := f.files[path]
	if !ok {
		return nil, false, fmt.Errorf("read %s: no such file", path)
	}
	return []byte(body), false, nil
}

func (f *fakeHost) Check(ctx context.Context) error              { return nil }
func (f *fakeHost) Close() error                                 { return nil }
func (f *fakeHost) Facts() (string, bool)                        { return "linux", false }
func (f *fakeHost) Connect(context.Context, int64) (Host, error) { return f, nil }

func testDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedNode inserts a Node directly. The collector under test never dials, so no
// credential or host key is needed.
func seedNode(t *testing.T, db *store.DB) int64 {
	t.Helper()
	id, err := db.AddNode(context.Background(), "10.90.4.2", 22, "web02", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

const nginxDump = `# configuration file /etc/nginx/nginx.conf:
worker_processes auto;
http {
    upstream app {
        server app01:8080;
        server 10.90.4.7:8443 backup;
    }
    include /etc/nginx/conf.d/*.conf;
}

# configuration file /etc/nginx/conf.d/site.conf:
server {
    listen 443 ssl;
    server_name shop.example.com;
    ssl_certificate /etc/nginx/certs/shop.crt;
    add_header X-Frame-Options DENY;
    location /api/ {
        proxy_pass http://app;
    }
    location / {
        root /var/www;
    }
}
`

func nginxFake() *fakeHost {
	return &fakeHost{
		out: map[sshx.ID]string{
			sshx.CmdUname:     "Linux 6.1.0",
			sshx.CmdSudoCheck: "",
			sshx.CmdProcList: "1\tnginx: master process /usr/sbin/nginx -g daemon off;\n" +
				"7\tnginx: worker process\n",
			sshx.CmdSystemdList:  "",
			sshx.CmdWhich:        "/usr/sbin/nginx\n",
			sshx.CmdNginxVersion: "nginx version: nginx/1.24.0\nconfigure arguments: --conf-path=/etc/nginx/nginx.conf\n",
			sshx.CmdNginxDump:    nginxDump,
			sshx.CmdGetentHosts:  "10.90.4.5       app01\n",
			sshx.CmdX509: "subject=CN = shop.example.com\n" +
				"issuer=CN = nagipath test CA\n" +
				"serial=01AB\n" +
				"notBefore=Jan  1 00:00:00 2025 GMT\n" +
				"notAfter=Jan  1 00:00:00 2027 GMT\n" +
				"sha256 Fingerprint=AA:BB:CC\n" +
				"DNS:shop.example.com, DNS:www.example.com\n",
		},
	}
}

func TestCollectNginxEndToEnd(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	host := nginxFake()

	c := &Collector{DB: db, Dialer: host}
	if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
		t.Fatalf("collect: %v", err)
	}

	instances, err := db.Instances(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("want 1 instance, got %d: %+v", len(instances), instances)
	}
	in := instances[0]
	if in.Vendor != "nginx" || in.Version != "nginx/1.24.0" {
		t.Errorf("instance = %+v", in)
	}
	if in.MainConfigPath != "/etc/nginx/nginx.conf" {
		t.Errorf("main config = %q", in.MainConfigPath)
	}
	if in.ServiceManager != "container" {
		t.Errorf("PID 1 with no systemd should read as a container, got %q", in.ServiceManager)
	}

	snap, err := db.CurrentSnapshot(ctx, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ConfigSource != "vendor_dump" {
		t.Errorf("config source = %q, want vendor_dump", snap.ConfigSource)
	}
	if snap.ParseState != "parsed" {
		t.Fatalf("parse state = %q (%s)", snap.ParseState, snap.ParseError)
	}

	// nginx -T is split back into the two files it printed, plus the dump itself.
	files, err := db.SnapshotFiles(ctx, snap.ID)
	if err != nil {
		t.Fatal(err)
	}
	var configs []string
	for _, f := range files {
		if f.Kind == "config_file" {
			configs = append(configs, f.Path)
		}
	}
	if len(configs) != 2 {
		t.Fatalf("want 2 config files from the dump, got %v", configs)
	}
	if configs[0] != "/etc/nginx/conf.d/site.conf" || configs[1] != "/etc/nginx/nginx.conf" {
		t.Errorf("config paths = %v", configs)
	}
}

// A dump that arrived on the other stream used to be indistinguishable from a
// host with no configuration: the collector read stdout, found it empty, and
// quietly replaced the authoritative dump with a filesystem walk.
func TestCollectReadsADumpThatArrivesOnStderr(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	host := nginxFake()
	host.onStderr = map[sshx.ID]bool{sshx.CmdNginxDump: true, sshx.CmdNginxVersion: true}

	c := &Collector{DB: db, Dialer: host}
	if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
		t.Fatalf("collect: %v", err)
	}
	instances, err := db.Instances(ctx)
	if err != nil || len(instances) != 1 {
		t.Fatalf("want 1 instance, got %d (%v)", len(instances), err)
	}
	if instances[0].Version != "nginx/1.24.0" {
		t.Errorf("version = %q; nginx prints -V to stderr", instances[0].Version)
	}
	snap, err := db.CurrentSnapshot(ctx, instances[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ConfigSource != "vendor_dump" {
		t.Errorf("config source = %q, want vendor_dump: the dump was there, on the other stream",
			snap.ConfigSource)
	}
	if snap.Degraded {
		t.Errorf("snapshot reported degraded: %s", snap.DegradedReason)
	}
}

// A process title is usually a bare name, and a non-interactive SSH session's
// PATH is not the daemon's PATH. Running the bare name is how a source-built
// httpd reports itself as "command not found".
func TestAbsBinaryResolvesAgainstDiscoveredPaths(t *testing.T) {
	bins := []string{"/usr/sbin/nginx", "/usr/local/apache2/bin/httpd"}
	cases := map[string]string{
		"httpd":                  "/usr/local/apache2/bin/httpd",
		"nginx":                  "/usr/sbin/nginx",
		"/opt/custom/sbin/nginx": "/opt/custom/sbin/nginx", // absolute already: leave it
		"haproxy":                "haproxy",                // nothing found: say what we saw
		"":                       "",
	}
	for in, want := range cases {
		if got := absBinary(in, bins); got != want {
			t.Errorf("absBinary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCollectPersistsDerivedRows(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	c := &Collector{DB: db, Dialer: nginxFake()}
	if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
		t.Fatalf("collect: %v", err)
	}

	count := func(q string) int {
		var n int
		if err := db.R.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM listener WHERE tls = 1 AND port = 443`); n != 1 {
		t.Errorf("tls listener rows = %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM site_name WHERE name = 'shop.example.com'`); n != 1 {
		t.Errorf("site_name rows = %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM upstream_member`); n != 2 {
		t.Errorf("upstream_member rows = %d, want 2", n)
	}
	if n := count(`SELECT COUNT(*) FROM route`); n != 2 {
		t.Errorf("route rows = %d, want 2", n)
	}

	// Every derived row must be anchored to a real snapshot_file byte range.
	if n := count(`SELECT COUNT(*) FROM route r JOIN snapshot_file f ON f.id = r.prov_file_id
		WHERE r.prov_byte_end > r.prov_byte_start`); n != 2 {
		t.Errorf("routes with usable provenance = %d, want 2", n)
	}
	// The parser version is on every row, so a reparse can find stale ones.
	if n := count(fmt.Sprintf(`SELECT COUNT(*) FROM rule WHERE parser_version != %d`, store.ParserVersion)); n != 0 {
		t.Errorf("%d rules carry the wrong parser version", n)
	}
}

func TestCollectResolvesNamesOnTheTarget(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	c := &Collector{DB: db, Dialer: nginxFake()}
	if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
		t.Fatal(err)
	}
	names, err := db.ResolvedNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := names["app01"]; len(got) != 1 || got[0] != "10.90.4.5" {
		t.Errorf("app01 resolved to %v, want [10.90.4.5]", got)
	}
	// A literal IP member is not a name and must not become a DNS record.
	if _, ok := names["10.90.4.7"]; ok {
		t.Error("an IP address was recorded as a resolved name")
	}
}

func TestCollectStoresCertificateMetadataOnly(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	c := &Collector{DB: db, Dialer: nginxFake()}
	if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
		t.Fatal(err)
	}
	certs, err := db.Certificates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(certs) != 1 {
		t.Fatalf("want 1 certificate, got %d", len(certs))
	}
	got := certs[0]
	if got.Fingerprint != "aabbcc" {
		t.Errorf("fingerprint = %q", got.Fingerprint)
	}
	if got.SubjectCN != "shop.example.com" {
		t.Errorf("subject CN = %q", got.SubjectCN)
	}
	if got.NotAfter != "2027-01-01T00:00:00Z" {
		t.Errorf("not_after = %q, want a sortable UTC timestamp", got.NotAfter)
	}
	if len(got.SANs) != 2 {
		t.Errorf("sans = %v", got.SANs)
	}
	if got.Bindings != 1 {
		t.Errorf("bindings on the current snapshot = %d", got.Bindings)
	}
}

// A second collection of an unchanged config must reuse the blobs and leave
// exactly one current Snapshot.
func TestCollectTwiceKeepsOneCurrentSnapshot(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	c := &Collector{DB: db, Dialer: nginxFake()}
	for i := 0; i < 2; i++ {
		if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
			t.Fatalf("collect %d: %v", i, err)
		}
	}
	var snapshots, current, blobs int
	db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot`).Scan(&snapshots)
	db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM snapshot WHERE is_current = 1`).Scan(&current)
	db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM blob`).Scan(&blobs)
	if snapshots != 2 || current != 1 {
		t.Errorf("snapshots = %d, current = %d", snapshots, current)
	}
	if blobs != 3 {
		t.Errorf("blobs = %d, want 3 shared across both snapshots", blobs)
	}
}

func TestCollectFallsBackWhenDumpUnavailable(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	nodeID := seedNode(t, db)
	host := nginxFake()
	delete(host.out, sshx.CmdNginxDump) // exit 127, as if nginx -T were denied
	host.files = map[string]string{
		"/etc/nginx/nginx.conf": "events {}\nhttp {\n  server { listen 80; server_name fallback.example.com; }\n}\n",
	}
	host.out[sshx.CmdFindConf] = "/etc/nginx/nginx.conf\n"

	c := &Collector{DB: db, Dialer: host}
	if err := c.Node(ctx, nodeID, "manual", nil); err != nil {
		t.Fatal(err)
	}
	instances, _ := db.Instances(ctx)
	if len(instances) != 1 {
		t.Fatalf("want 1 instance, got %d", len(instances))
	}
	snap, err := db.CurrentSnapshot(ctx, instances[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ConfigSource != "fallback_walk" {
		t.Errorf("config source = %q, want fallback_walk", snap.ConfigSource)
	}
	if !snap.Degraded {
		t.Error("a fallback walk cannot prove completeness and must be marked degraded")
	}
	cols, err := db.Collections(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cols[0].Status != "degraded" {
		t.Errorf("collection status = %q, want degraded", cols[0].Status)
	}
}

// TestLogPathsHAProxyFallback covers the three shapes HAProxy logging comes in: an
// explicit file directive (used as-is), a syslog socket or stdout (neither is a
// file nagipath can tail, so it falls back to the conventional defaults, dedicated
// files first and the shared syslog last), and — for every other vendor — no
// change to the existing access_log/CustomLog behaviour.
func TestLogPathsHAProxyFallback(t *testing.T) {
	rule := func(directive, args string) *parse.Rule {
		return &parse.Rule{Directive: directive, Args: args}
	}

	cases := []struct {
		name string
		res  *parse.Result
		want []string
	}{
		{
			name: "haproxy explicit file directive is used as-is",
			res: &parse.Result{Vendor: "haproxy",
				GlobalRules: []*parse.Rule{rule("log", "/var/log/lb-custom.log local0")}},
			want: []string{"/var/log/lb-custom.log"},
		},
		{
			name: "haproxy syslog socket falls back to the conventional defaults",
			res: &parse.Result{Vendor: "haproxy",
				GlobalRules: []*parse.Rule{rule("log", "/dev/log local0")}},
			want: []string{"/var/log/haproxy.log", "/var/log/haproxy/haproxy.log",
				"/var/log/syslog", "/var/log/messages"},
		},
		{
			name: "haproxy stdout falls back to the conventional defaults",
			res: &parse.Result{Vendor: "haproxy",
				GlobalRules: []*parse.Rule{rule("log", "stdout format raw local0")}},
			want: []string{"/var/log/haproxy.log", "/var/log/haproxy/haproxy.log",
				"/var/log/syslog", "/var/log/messages"},
		},
		{
			name: "nginx access_log is unaffected",
			res: &parse.Result{Vendor: "nginx",
				GlobalRules: []*parse.Rule{rule("access_log", "/var/log/nginx/access.log combined")}},
			want: []string{"/var/log/nginx/access.log"},
		},
		{
			name: "apache CustomLog pointed at /proc/self/fd is not a file nagipath can tail",
			res: &parse.Result{Vendor: "apache",
				GlobalRules: []*parse.Rule{rule("customlog", "/proc/self/fd/1 fleet")}},
			want: nil,
		},
		{
			name: "a numeric pid's fd path is excluded the same way self is",
			res: &parse.Result{Vendor: "apache",
				GlobalRules: []*parse.Rule{rule("customlog", "/proc/1/fd/1 fleet")}},
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := logPaths(c.res)
			if len(got) != len(c.want) {
				t.Fatalf("logPaths() = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("logPaths()[%d] = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}
