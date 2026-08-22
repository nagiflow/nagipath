package trace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/parse"
)

// The lab is documentation, and documentation that is wrong is worse than none.
// This builds the fleet from testlab's real configuration files and asserts every
// claim testlab/README.md makes about the trace — without needing Docker, so it
// runs in CI and fails the moment a config edit invalidates the prose.
func TestLabConfigsProduceTheDocumentedTrace(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	lab := func(rel string) string {
		path := filepath.Join("..", "..", "testlab", rel)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("the lab is missing %s: %v", rel, err)
		}
		return string(body)
	}

	// The lab's configurations are trees, so the fixture has to be one too. Each
	// entry maps a file in testlab/ to the path it is mounted at in the container,
	// because the parser resolves `include` against the target's paths — a tree
	// seeded under the wrong prefix parses as a pile of unreachable files.
	tree := func(node string, mounts map[string]string) []parse.File {
		var files []parse.File
		for rel, target := range mounts {
			dir := filepath.Join("..", "..", "testlab", node, rel)
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("the lab is missing %s/%s: %v", node, rel, err)
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
					continue
				}
				files = append(files, parse.File{
					Path:    target + "/" + e.Name(),
					Content: []byte(lab(filepath.Join(node, rel, e.Name()))),
				})
			}
		}
		return files
	}

	nginxTree := func(node string) []parse.File {
		files := []parse.File{{
			Path:    "/etc/nginx/nginx.conf",
			Content: []byte(lab(node + "/nginx.conf")),
		}}
		files = append(files, tree(node, map[string]string{
			"conf.d":        "/etc/nginx/conf.d",
			"snippets":      "/etc/nginx/snippets",
			"sites-enabled": "/etc/nginx/sites-enabled",
		})...)
		// mime.types and the root-only include are real files on the node that the
		// repository does not carry. Without them the parse degrades on a missing
		// include, which is correct behaviour and not what this test is about.
		files = append(files,
			parse.File{Path: "/etc/nginx/mime.types", Content: []byte("types { text/html html; }")},
			parse.File{Path: "/etc/nginx/lab-tuning.conf", Content: []byte("client_max_body_size 32m;")},
		)
		return files
	}

	seed(t, db, "lb01", "lb01", "haproxy", "/usr/local/etc/haproxy/haproxy.cfg", lab("lb01/haproxy.cfg"))
	seedFiles(t, db, "web02", "web02", "nginx", "/etc/nginx/nginx.conf", nginxTree("web02"))
	seedFiles(t, db, "web05", "web05", "nginx", "/etc/nginx/nginx.conf", nginxTree("web05"))

	apacheFiles := []parse.File{{
		Path:    "/usr/local/apache2/conf/httpd.conf",
		Content: []byte(lab("app01/httpd.conf")),
	}}
	apacheFiles = append(apacheFiles, tree("app01", map[string]string{
		"conf.d":        "/usr/local/apache2/conf/conf.d",
		"sites-enabled": "/usr/local/apache2/conf/sites-enabled",
	})...)
	apacheFiles = append(apacheFiles, parse.File{
		Path: "/usr/local/apache2/conf/mime.types", Content: []byte("text/html html\n")})
	seedFiles(t, db, "app01", "app01", "apache", "/usr/local/apache2/conf/httpd.conf", apacheFiles)

	top, err := Load(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	tr := Walk(top, Query{Scheme: "https", Hostname: "shop.example.com", Path: "/api/v2/charge"})

	// lb01 is the edge, not one of the things it proxies to.
	if len(tr.Hops) == 0 {
		t.Fatalf("the lab traced nothing: %s (%s)", tr.TerminalReason, strings.Join(tr.Notes, "; "))
	}
	if got := tr.Hops[0].Inst.DisplayName; !strings.HasPrefix(got, "lb01") {
		t.Errorf("hop 1 = %s, want lb01", got)
	}
	if !tr.Hops[0].Listener.TLS || tr.Hops[0].Listener.Port != 443 {
		t.Errorf("hop 1 listener = :%d tls=%v, want :443 with TLS",
			tr.Hops[0].Listener.Port, tr.Hops[0].Listener.TLS)
	}

	// Both conditional use_backend lines are branches, not decisions.
	var branches []string
	for _, b := range tr.Undetermined {
		branches = append(branches, b.Raw)
	}
	joined := strings.Join(branches, "\n")
	for _, want := range []string{"be_static", "be_admin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("use_backend %s is not listed as undetermined; got:\n%s", want, joined)
		}
	}

	// web02 rewrites the path; the trailing slash on proxy_pass is the whole point.
	web02 := hopFor(tr, "web02")
	if web02 == nil {
		t.Fatal("the trace never reached web02")
	}
	if web02.InboundPath != "/api/v2/charge" || web02.EffectivePath != "/v2/charge" {
		t.Errorf("web02 path %s -> %s, want /api/v2/charge -> /v2/charge",
			web02.InboundPath, web02.EffectivePath)
	}
	if len(web02.PathChangedBy) == 0 {
		t.Error("web02 changed the path but does not say which rule did it")
	}

	// The inner add_header discards both server-level ones.
	var discarded []string
	for _, r := range web02.Shadowed {
		discarded = append(discarded, r.Rule.Args)
	}
	shadowed := strings.Join(discarded, "\n")
	for _, want := range []string{"X-Frame-Options", "X-Served-By"} {
		if !strings.Contains(shadowed, want) {
			t.Errorf("%s should be reported as discarded at web02; got:\n%s", want, shadowed)
		}
	}

	// app01 serves it from disk, having chosen <Location> over <Directory>.
	app01 := hopFor(tr, "app01")
	if app01 == nil {
		t.Fatal("the trace never reached app01")
	}
	if app01.InboundPath != "/v2/charge" {
		t.Errorf("app01 received %s, want /v2/charge", app01.InboundPath)
	}
	if app01.Route == nil || !strings.HasPrefix(app01.Route.MatchType, "location") {
		t.Errorf("app01 route = %#v, want a <Location> match (Apache merges, last wins)", app01.Route)
	}
	if app01.Terminal != TermStatic {
		t.Errorf("app01 terminal = %q, want %q", app01.Terminal, TermStatic)
	}

	// The README's headline claim about the wire: web02's X-Frame-Options is
	// discarded and app01's weaker one is what the client gets. Asserting that
	// app01 still applies its own is what stops that sentence going stale.
	var app01Applied []string
	for _, r := range app01.Rules {
		app01Applied = append(app01Applied, r.Rule.Directive+" "+r.Rule.Args)
	}
	if !strings.Contains(strings.Join(app01Applied, "\n"), "X-Frame-Options") {
		t.Errorf("app01 should apply its own X-Frame-Options; got:\n%s",
			strings.Join(app01Applied, "\n"))
	}

	// web05 is the second member of be_app and dies at the empty address.
	web05 := hopFor(tr, "web05")
	if web05 == nil {
		t.Fatal("web05 is a member of be_app but never became a hop")
	}
	if !strings.Contains(strings.Join(rawTargets(web05), " "), "10.90.4.7") {
		t.Errorf("web05 should proxy to the unreachable 10.90.4.7; got %v", rawTargets(web05))
	}
}

func hopFor(tr *Trace, name string) *Hop {
	for _, h := range tr.Hops {
		if h.Inst != nil && strings.HasPrefix(h.Inst.DisplayName, name) {
			return h
		}
	}
	return nil
}

// rawTargets is where the hop was heading, whether that is a managed next hop or
// an address nothing answers on.
func rawTargets(h *Hop) []string {
	var out []string
	for _, n := range h.Next {
		out = append(out, n.Member.Host)
	}
	if h.ExternalTarget != "" {
		out = append(out, h.ExternalTarget)
	}
	if h.Upstream != nil {
		for _, m := range h.Upstream.Members {
			out = append(out, m.Host)
		}
	}
	return out
}
