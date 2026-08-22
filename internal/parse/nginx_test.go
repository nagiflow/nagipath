package parse

import (
	"strings"
	"testing"
)

// The cases here are the ones where being wrong is invisible until a customer is
// misled: regex ordering, the ^~ short-circuit, add_header discard, include
// globbing, and byte-exact provenance.

const mainConf = `
user www-data;
http {
    log_format main '$remote_addr $request';

    upstream payments {
        least_conn;
        server 10.0.0.11:8080 weight=3;
        server 10.0.0.12:8080;
    }

    include conf.d/*.conf;
}
`

const siteConf = `
server {
    listen 443 ssl http2 default_server;
    server_name shop.example.com *.shop.example.com;
    root /var/www/shop;

    add_header X-Frame-Options DENY;
    ssl_certificate     /etc/ssl/shop.crt;
    ssl_certificate_key /etc/ssl/shop.key;

    location = /healthz          { return 200 "ok"; }
    location ^~ /assets/         { root /var/www/assets; }
    location ~ /api/v[0-9]+/     { proxy_pass http://payments; }
    location ~ /api/v2/specific/ { proxy_pass http://payments; }
    location /                   { proxy_pass http://10.0.0.99:9000; }

    location /secure/ {
        add_header X-Secure yes;
        auth_basic "restricted";
        totally_unknown_directive foo bar;
    }
}
`

func parseLab(t *testing.T) *Result {
	t.Helper()
	res := NGINX([]File{
		{Path: "/etc/nginx/nginx.conf", Content: []byte(mainConf)},
		{Path: "/etc/nginx/conf.d/shop.conf", Content: []byte(siteConf)},
	}, "/etc/nginx/nginx.conf")
	if res.Degraded {
		t.Fatalf("parse degraded: %s", res.DegradedReason)
	}
	return res
}

func TestNginxSiteAndListener(t *testing.T) {
	res := parseLab(t)
	if len(res.Sites) != 1 {
		t.Fatalf("want 1 site, got %d", len(res.Sites))
	}
	site := res.Sites[0]
	if site.PrimaryName != "shop.example.com" {
		t.Errorf("primary name = %q", site.PrimaryName)
	}
	if len(site.Names) != 2 || site.Names[1].MatchKind != "wildcard_prefix" {
		t.Errorf("server_name kinds = %+v", site.Names)
	}
	if site.DocumentRoot != "/var/www/shop" {
		t.Errorf("document root = %q", site.DocumentRoot)
	}
	if len(res.Listeners) != 1 {
		t.Fatalf("want 1 listener, got %d", len(res.Listeners))
	}
	l := res.Listeners[0]
	if l.Port != 443 || !l.TLS || l.Protocol != "http2" || !l.IsDefault {
		t.Errorf("listener = %+v", l)
	}
}

// Regex locations are resolved by FILE ORDER, not by pattern length. Rank 30 must
// carry specificity 0, and the earlier regex must sort first. Getting this
// backwards is the single most likely precedence bug.
func TestNginxRegexOrderIsFileOrder(t *testing.T) {
	site := parseLab(t).Sites[0]
	var regexes []*Route
	for _, r := range site.Routes {
		if strings.HasPrefix(r.MatchType, "regex") {
			regexes = append(regexes, r)
		}
	}
	if len(regexes) != 2 {
		t.Fatalf("want 2 regex routes, got %d", len(regexes))
	}
	for _, r := range regexes {
		if r.PrecedenceRank != RankNginxRegex {
			t.Errorf("%s: rank = %d, want %d", r.Pattern, r.PrecedenceRank, RankNginxRegex)
		}
		if r.Specificity != 0 {
			t.Errorf("%s: specificity = %d, want 0 — regex order is file order",
				r.Pattern, r.Specificity)
		}
	}
	if regexes[0].Pattern != "/api/v[0-9]+/" {
		t.Errorf("first regex = %q, want the one declared first", regexes[0].Pattern)
	}
}

// ^~ is a short-circuit, not a score: it must outrank every regex regardless of
// how much longer the regex pattern is.
func TestNginxPrefixNoRegexBeatsRegex(t *testing.T) {
	site := parseLab(t).Sites[0]
	var noRE, re *Route
	for _, r := range site.Routes {
		switch r.MatchType {
		case "prefix_no_regex":
			noRE = r
		case "regex":
			if re == nil {
				re = r
			}
		}
	}
	if noRE == nil || re == nil {
		t.Fatal("missing routes")
	}
	if noRE.PrecedenceRank >= re.PrecedenceRank {
		t.Errorf("^~ rank %d must be lower than regex rank %d",
			noRE.PrecedenceRank, re.PrecedenceRank)
	}
	if len(re.Pattern) <= len(noRE.Pattern) {
		t.Skip("fixture no longer has a longer regex; the assertion is meaningless")
	}
}

func TestNginxExactAndPrefixRanks(t *testing.T) {
	site := parseLab(t).Sites[0]
	want := map[string]int{
		"/healthz": RankNginxExact,
		"/assets/": RankNginxPrefixNoRE,
		"/":        RankNginxPrefix,
		"/secure/": RankNginxPrefix,
	}
	for _, r := range site.Routes {
		if w, ok := want[r.Pattern]; ok && r.PrecedenceRank != w {
			t.Errorf("%s: rank = %d, want %d", r.Pattern, r.PrecedenceRank, w)
		}
	}
	for _, r := range site.Routes {
		if r.Pattern == "/" && r.Specificity != 1 {
			t.Errorf("prefix specificity must be pattern length, got %d", r.Specificity)
		}
	}
}

// A nearer add_header discards every inherited one. A configured-but-ineffective
// header is the product's most valuable output, so it must be marked.
func TestNginxAddHeaderIsShadowed(t *testing.T) {
	site := parseLab(t).Sites[0]
	var found bool
	for _, rule := range site.Rules {
		if rule.Directive != "add_header" {
			continue
		}
		found = true
		if !rule.Shadowed {
			t.Error("site-scope add_header must be marked shadowed: /secure/ declares its own")
		}
		if !strings.Contains(rule.ShadowedBy, "/secure/") {
			t.Errorf("shadowed_by = %q, want it to name /secure/", rule.ShadowedBy)
		}
	}
	if !found {
		t.Fatal("no site-scope add_header rule captured")
	}
}

func TestNginxUpstreamAndInline(t *testing.T) {
	res := parseLab(t)
	var declared, inline *Upstream
	for _, up := range res.Upstreams {
		switch up.Kind {
		case "nginx_upstream":
			declared = up
		case "nginx_inline":
			inline = up
		}
	}
	if declared == nil {
		t.Fatal("declared upstream missing")
	}
	if declared.BalanceMethod != "least_conn" {
		t.Errorf("balance method = %q", declared.BalanceMethod)
	}
	if len(declared.Members) != 2 {
		t.Fatalf("want 2 members, got %d", len(declared.Members))
	}
	if declared.Members[0].Host != "10.0.0.11" || declared.Members[0].Port != 8080 {
		t.Errorf("member 0 = %+v", declared.Members[0])
	}
	if declared.Members[0].Weight != 3 {
		t.Errorf("weight = %d, want 3", declared.Members[0].Weight)
	}
	if inline == nil {
		t.Fatal("bare proxy_pass target must become an inline upstream")
	}
	if inline.Members[0].Host != "10.0.0.99" || inline.Members[0].Port != 9000 {
		t.Errorf("inline member = %+v", inline.Members[0])
	}
}

// Nothing is dropped: a directive we do not model is still a Rule, marked.
func TestNginxUnmodelledDirectiveSurvives(t *testing.T) {
	site := parseLab(t).Sites[0]
	for _, r := range site.Routes {
		if r.Pattern != "/secure/" {
			continue
		}
		for _, rule := range r.Rules {
			if rule.Directive == "totally_unknown_directive" {
				if rule.IsModelled {
					t.Error("unknown directive must have is_modelled = 0")
				}
				if !strings.Contains(rule.Raw, "foo bar") {
					t.Errorf("raw text lost: %q", rule.Raw)
				}
				return
			}
		}
	}
	t.Fatal("unmodelled directive was dropped")
}

// Provenance must point at the included file, at exact bytes.
func TestNginxProvenanceIsByteExact(t *testing.T) {
	site := parseLab(t).Sites[0]
	if site.Path != "/etc/nginx/conf.d/shop.conf" {
		t.Fatalf("site provenance path = %q, want the included file", site.Path)
	}
	got := siteConf[site.Start:site.End]
	if !strings.HasPrefix(got, "server {") || !strings.HasSuffix(got, "}") {
		t.Errorf("byte range does not bracket the server block: %.30q...", got)
	}
	for _, r := range site.Routes {
		slice := siteConf[r.Start:r.End]
		if !strings.HasPrefix(slice, "location") {
			t.Errorf("route %q provenance starts with %.20q", r.Pattern, slice)
		}
	}
}

// UTF-8 and CRLF must not shift offsets. Provenance off by a byte is provenance
// nobody trusts.
func TestNginxOffsetsSurviveCRLFAndUTF8(t *testing.T) {
	src := "http {\r\n  # café — configuração\r\n  server {\r\n    listen 8080;\r\n  }\r\n}\r\n"
	res := NGINX([]File{{Path: "/etc/nginx/nginx.conf", Content: []byte(src)}}, "/etc/nginx/nginx.conf")
	if len(res.Sites) != 1 {
		t.Fatalf("want 1 site, got %d", len(res.Sites))
	}
	s := res.Sites[0]
	if got := src[s.Start:s.End]; !strings.HasPrefix(got, "server {") {
		t.Errorf("offsets shifted: %.20q", got)
	}
}

func TestNginxMissingIncludeDegrades(t *testing.T) {
	res := NGINX([]File{{Path: "/etc/nginx/nginx.conf",
		Content: []byte("http { include missing/*.conf; }")}}, "/etc/nginx/nginx.conf")
	if !res.Degraded {
		t.Fatal("a missing include must degrade the parse, not pass silently")
	}
	if !strings.Contains(res.DegradedReason, "missing/*.conf") {
		t.Errorf("degraded reason must name the pattern, got %q", res.DegradedReason)
	}
}

func TestNginxCertificateBinding(t *testing.T) {
	res := parseLab(t)
	if len(res.CertBindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(res.CertBindings))
	}
	b := res.CertBindings[0]
	if b.CertPath != "/etc/ssl/shop.crt" || b.KeyPath != "/etc/ssl/shop.key" {
		t.Errorf("binding = %+v", b)
	}
}
