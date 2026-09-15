package parse

import (
	"strings"
	"testing"
)

const httpdConf = `
Listen 80
Listen 443 https
LoadModule proxy_module modules/mod_proxy.so

<Proxy balancer://payments>
    BalancerMember http://10.0.0.11:8080 loadfactor=2
    BalancerMember http://10.0.0.12:8080
    ProxySet lbmethod=byrequests
</Proxy>

Include conf.d/*.conf
`

const vhostConf = `
<VirtualHost *:443>
    ServerName app.example.com
    ServerAlias www.example.com
    DocumentRoot /var/www/app
    SSLEngine on
    SSLCertificateFile    /etc/ssl/app.crt
    SSLCertificateKeyFile /etc/ssl/app.key

    <Directory /var/www/app/private>
        Require all denied
    </Directory>

    <Location /private>
        Require valid-user
        AuthType Basic
    </Location>

    <FilesMatch "\.php$">
        SetHandler application/x-httpd-php
    </FilesMatch>

    RewriteEngine On
    RewriteRule ^/old/(.*)$ /new/$1 [L,R=301]

    ProxyPass /api balancer://payments/
    SomeUnknownDirective alpha beta
</VirtualHost>
`

func parseApacheLab(t *testing.T) *Result {
	t.Helper()
	res := Apache([]File{
		{Path: "/etc/httpd/conf/httpd.conf", Content: []byte(httpdConf)},
		{Path: "/etc/httpd/conf.d/vhost.conf", Content: []byte(vhostConf)},
	}, "/etc/httpd/conf/httpd.conf")
	if res.Degraded {
		t.Fatalf("parse degraded: %s", res.DegradedReason)
	}
	return res
}

// <Location> beats <Directory>. Reversing these is the Apache equivalent of
// getting NGINX regex ordering backwards.
func TestApacheLocationBeatsDirectory(t *testing.T) {
	site := parseApacheLab(t).Sites[0]
	var dir, loc, files *Route
	for _, r := range site.Routes {
		switch r.MatchType {
		case "directory":
			dir = r
		case "location":
			if r.Pattern == "/private" {
				loc = r
			}
		case "files_match":
			files = r
		}
	}
	if dir == nil || loc == nil || files == nil {
		t.Fatalf("missing routes: dir=%v loc=%v files=%v", dir, loc, files)
	}
	if !(dir.PrecedenceRank < files.PrecedenceRank && files.PrecedenceRank < loc.PrecedenceRank) {
		t.Errorf("merge order wrong: Directory %d, Files %d, Location %d",
			dir.PrecedenceRank, files.PrecedenceRank, loc.PrecedenceRank)
	}
	if dir.PrecedenceRank != RankApacheDirectory || loc.PrecedenceRank != RankApacheLocation {
		t.Errorf("ranks = %d / %d", dir.PrecedenceRank, loc.PrecedenceRank)
	}
}

func TestApacheVhostAndCertificate(t *testing.T) {
	res := parseApacheLab(t)
	if len(res.Sites) != 1 {
		t.Fatalf("want 1 vhost, got %d", len(res.Sites))
	}
	site := res.Sites[0]
	if site.PrimaryName != "app.example.com" {
		t.Errorf("primary name = %q", site.PrimaryName)
	}
	if len(site.Names) != 2 || site.Names[1].Name != "www.example.com" {
		t.Errorf("names = %+v", site.Names)
	}
	if site.DocumentRoot != "/var/www/app" {
		t.Errorf("document root = %q", site.DocumentRoot)
	}
	var l *Listener
	for _, cand := range res.Listeners {
		if cand.Port == 443 && cand.TLS {
			l = cand
		}
	}
	if l == nil {
		t.Fatalf("no TLS listener on 443: %+v", res.Listeners)
	}
	if len(res.CertBindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(res.CertBindings))
	}
	if res.CertBindings[0].CertPath != "/etc/ssl/app.crt" {
		t.Errorf("binding = %+v", res.CertBindings[0])
	}
	if res.CertBindings[0].CombinedPEM {
		t.Error("Apache bindings are never combined PEMs")
	}
}

// A relative Include is resolved against ServerRoot, not against the directory
// httpd.conf is in. On the stock httpd image those differ by one path element, so
// resolving against the wrong one finds nothing — and IncludeOptional makes that
// silent on both sides.
func TestApacheIncludeResolvesAgainstServerRoot(t *testing.T) {
	main := `
ServerRoot /usr/local/apache2
Listen 8080
IncludeOptional conf.d/*.conf
`
	// The file is where ServerRoot says to look, which is not next to httpd.conf.
	res := Apache([]File{
		{Path: "/usr/local/apache2/conf/httpd.conf", Content: []byte(main)},
		{Path: "/usr/local/apache2/conf.d/site.conf", Content: []byte(vhostConf)},
	}, "/usr/local/apache2/conf/httpd.conf")
	if res.Degraded {
		t.Fatalf("parse degraded: %s", res.DegradedReason)
	}
	if len(res.Sites) != 1 {
		t.Fatalf("the include under ServerRoot was not loaded: %d sites", len(res.Sites))
	}

	// The same include with the file where the config directory is instead. Apache
	// would load nothing here, so reporting a complete parse would be a lie.
	res = Apache([]File{
		{Path: "/usr/local/apache2/conf/httpd.conf", Content: []byte(main)},
		{Path: "/usr/local/apache2/conf/conf.d/site.conf", Content: []byte(vhostConf)},
	}, "/usr/local/apache2/conf/httpd.conf")
	if !res.Degraded {
		t.Error("an include that only resolves outside ServerRoot must degrade the parse")
	}
	if !strings.Contains(res.DegradedReason, "ServerRoot") {
		t.Errorf("degraded reason does not name the ambiguity: %q", res.DegradedReason)
	}
}

// <IfModule> is decided when Apache reads the file, so its contents are ordinary
// configuration — including whole vhosts. Treating the container as one opaque
// directive drops everything inside it, and real Apache configurations put most of
// what they do inside one.
func TestApacheConditionalContainerIsHoisted(t *testing.T) {
	res := Apache([]File{{Path: "/etc/httpd/conf/httpd.conf", Content: []byte(`
Listen 443 https
<IfModule headers_module>
    Header set X-Frame-Options "SAMEORIGIN"
</IfModule>
<IfDefine PROD>
` + vhostConf + `
</IfDefine>
`)}}, "/etc/httpd/conf/httpd.conf")
	if res.Degraded {
		t.Fatalf("parse degraded: %s", res.DegradedReason)
	}
	if len(res.Sites) != 1 {
		t.Fatalf("the vhost inside <IfDefine> is missing: %d sites", len(res.Sites))
	}

	var header, guard bool
	for _, r := range res.GlobalRules {
		switch lower(r.Directive) {
		case "header":
			header = strings.Contains(r.Args, "X-Frame-Options")
		case "ifmodule":
			// The guard is kept next to what it guards: whether the module is loaded
			// is a fact about the running server, not about the text.
			guard = true
		}
	}
	if !header {
		t.Error("the Header inside <IfModule> was dropped")
	}
	if !guard {
		t.Error("the <IfModule> guard itself must stay visible in the inventory")
	}
}

func TestApacheBalancerAndProxyPass(t *testing.T) {
	res := parseApacheLab(t)
	var bal *Upstream
	for _, up := range res.Upstreams {
		if up.Kind == "apache_balancer" {
			bal = up
		}
	}
	if bal == nil {
		t.Fatal("balancer upstream missing")
	}
	if bal.Name != "payments" || bal.BalanceMethod != "byrequests" {
		t.Errorf("balancer = %+v", bal)
	}
	if len(bal.Members) != 2 || bal.Members[0].Port != 8080 || bal.Members[0].Weight != 2 {
		t.Errorf("members = %+v", bal.Members[0])
	}
	var proxied *Route
	for _, r := range res.Sites[0].Routes {
		if r.Pattern == "/api" {
			proxied = r
		}
	}
	if proxied == nil {
		t.Fatal("ProxyPass did not become a Route")
	}
	if proxied.UpstreamKey != "balancer:payments" {
		t.Errorf("upstream key = %q", proxied.UpstreamKey)
	}
}

func TestApacheRewriteLastAndUnknownDirective(t *testing.T) {
	site := parseApacheLab(t).Sites[0]
	var sawRewrite, sawUnknown bool
	for _, rule := range site.Rules {
		switch rule.Directive {
		case "RewriteRule":
			sawRewrite = true
			if rule.ActionClass != ClassRewrite {
				t.Errorf("RewriteRule class = %q", rule.ActionClass)
			}
		case "SomeUnknownDirective":
			sawUnknown = true
			if rule.IsModelled {
				t.Error("unknown directive must have is_modelled = 0")
			}
			if !strings.Contains(rule.Raw, "alpha beta") {
				t.Errorf("raw text lost: %q", rule.Raw)
			}
		}
	}
	if !sawRewrite || !sawUnknown {
		t.Fatalf("rewrite=%v unknown=%v", sawRewrite, sawUnknown)
	}
}

func TestApacheProvenanceIsByteExact(t *testing.T) {
	site := parseApacheLab(t).Sites[0]
	if site.Path != "/etc/httpd/conf.d/vhost.conf" {
		t.Fatalf("provenance path = %q", site.Path)
	}
	got := vhostConf[site.Start:site.End]
	if !strings.HasPrefix(got, "<VirtualHost") {
		t.Errorf("byte range starts at %.20q", got)
	}
	if !strings.Contains(got, "</VirtualHost>") {
		t.Error("byte range does not cover the whole section")
	}
}

// ------------------------------------------------------------------ haproxy

const haproxyCfg = `
global
    log stdout format raw local0

defaults
    mode http
    timeout connect 5s

frontend https_in
    bind *:443 ssl crt /etc/haproxy/certs/shop.pem
    bind *:80
    acl is_shop hdr(host) -i shop.example.com
    acl is_api  path_beg /api
    http-request set-header X-Forwarded-Proto https
    use_backend api_servers if is_api
    default_backend web_servers
    weird_unknown_keyword yes

backend api_servers
    balance leastconn
    server app01 10.90.4.5:9000 check weight 10
    server extern 10.90.4.7:8443 check

backend web_servers
    server web02 10.90.4.2:8080 check
`

func parseHAProxyLab(t *testing.T) *Result {
	t.Helper()
	res := HAProxy([]File{{Path: "/etc/haproxy/haproxy.cfg", Content: []byte(haproxyCfg)}})
	if res.Degraded {
		t.Fatalf("parse degraded: %s", res.DegradedReason)
	}
	return res
}

// use_backend (90) is evaluated before default_backend (99).
func TestHAProxyBackendPrecedence(t *testing.T) {
	site := parseHAProxyLab(t).Sites[0]
	var use, def *Route
	for _, r := range site.Routes {
		switch r.MatchType {
		case "haproxy_acl_use_backend":
			use = r
		case "haproxy_default_backend":
			def = r
		}
	}
	if use == nil || def == nil {
		t.Fatalf("routes = %+v", site.Routes)
	}
	if use.PrecedenceRank != RankHAProxyUseBack || def.PrecedenceRank != RankHAProxyDefault {
		t.Errorf("ranks = %d / %d", use.PrecedenceRank, def.PrecedenceRank)
	}
	if use.PrecedenceRank >= def.PrecedenceRank {
		t.Error("use_backend must be evaluated before default_backend")
	}
	if use.Pattern != "is_api" {
		t.Errorf("ACL condition not captured: %q", use.Pattern)
	}
	if use.UpstreamKey != "backend:api_servers" {
		t.Errorf("upstream key = %q", use.UpstreamKey)
	}
}

// `crt` points at a combined PEM. We record that fact and never read the file, so
// no key material can enter the database.
func TestHAProxyCombinedPEM(t *testing.T) {
	res := parseHAProxyLab(t)
	if len(res.CertBindings) != 1 {
		t.Fatalf("want 1 binding, got %d", len(res.CertBindings))
	}
	b := res.CertBindings[0]
	if !b.CombinedPEM {
		t.Error("haproxy crt must be marked as a combined PEM")
	}
	if b.KeyPath != "" {
		t.Errorf("key path must stay empty, got %q", b.KeyPath)
	}
	for _, s := range []string{b.CertPath, b.SiteKey} {
		if strings.Contains(s, "PRIVATE KEY") {
			t.Fatal("key material leaked into a binding")
		}
	}
}

func TestHAProxyListenersAndNames(t *testing.T) {
	res := parseHAProxyLab(t)
	if len(res.Listeners) != 2 {
		t.Fatalf("want 2 listeners, got %d: %+v", len(res.Listeners), res.Listeners)
	}
	var tlsL, plainL *Listener
	for _, l := range res.Listeners {
		if l.TLS {
			tlsL = l
		} else {
			plainL = l
		}
	}
	if tlsL == nil || tlsL.Port != 443 {
		t.Errorf("tls listener = %+v", tlsL)
	}
	if plainL == nil || plainL.Port != 80 {
		t.Errorf("plain listener = %+v", plainL)
	}
	site := res.Sites[0]
	if len(site.Names) != 1 || site.Names[0].Name != "shop.example.com" {
		t.Errorf("names from host ACL = %+v", site.Names)
	}
}

func TestHAProxyBackendMembers(t *testing.T) {
	res := parseHAProxyLab(t)
	byName := map[string]*Upstream{}
	for _, up := range res.Upstreams {
		byName[up.Name] = up
	}
	api := byName["api_servers"]
	if api == nil {
		t.Fatal("api_servers backend missing")
	}
	if api.BalanceMethod != "leastconn" {
		t.Errorf("balance = %q", api.BalanceMethod)
	}
	if len(api.Members) != 2 {
		t.Fatalf("want 2 members, got %d", len(api.Members))
	}
	if api.Members[0].Host != "10.90.4.5" || api.Members[0].Port != 9000 || api.Members[0].Weight != 10 {
		t.Errorf("member 0 = %+v", api.Members[0])
	}
	// The unreachable external target must be present as a member, not dropped:
	// the Trace has to terminate at an External Hop rather than claim completeness.
	if api.Members[1].Host != "10.90.4.7" || api.Members[1].Port != 8443 {
		t.Errorf("member 1 = %+v", api.Members[1])
	}
}

func TestHAProxyUnknownKeywordSurvives(t *testing.T) {
	site := parseHAProxyLab(t).Sites[0]
	for _, rule := range site.Rules {
		if rule.Directive == "weird_unknown_keyword" {
			if rule.IsModelled {
				t.Error("unknown keyword must have is_modelled = 0")
			}
			return
		}
	}
	t.Fatal("unknown keyword was dropped")
}

func TestHAProxyProvenance(t *testing.T) {
	res := parseHAProxyLab(t)
	site := res.Sites[0]
	got := haproxyCfg[site.Start:site.End]
	if !strings.HasPrefix(got, "frontend https_in") {
		t.Errorf("frontend byte range starts at %.25q", got)
	}
	for _, rule := range site.Rules {
		slice := haproxyCfg[rule.Start:rule.End]
		if !strings.Contains(slice, rule.Directive) {
			t.Errorf("rule %q provenance points at %.30q", rule.Directive, slice)
		}
	}
}

// The silent failure this is here to prevent: a collection that succeeds, a
// parse that reports no problem, and an inventory with no sites in it, because
// a relative IncludeOptional resolved somewhere the captured files are not.
// Two halves — the common layout must actually load, and anything still
// unreached must say so by name.
func TestApacheIncludeOptionalFindsConfD(t *testing.T) {
	// RHEL's layout with no explicit ServerRoot: the compiled-in default is
	// /etc/httpd, not the conf/ directory httpd.conf lives in.
	res := Apache([]File{
		{Path: "/etc/httpd/conf/httpd.conf", Content: []byte("Listen 80\nIncludeOptional conf.d/*.conf\n")},
		{Path: "/etc/httpd/conf.d/site.conf", Content: []byte(vhostConf)},
	}, "/etc/httpd/conf/httpd.conf")
	if res.Degraded {
		t.Fatalf("parse degraded: %s", res.DegradedReason)
	}
	if len(res.Sites) != 1 {
		t.Fatalf("conf.d/*.conf under the default ServerRoot was not loaded: %d sites", len(res.Sites))
	}

	// A captured file no Include reaches is configuration the server is not
	// running. IncludeOptional makes Apache silent about it; the parse must not be.
	res = Apache([]File{
		{Path: "/etc/httpd/conf/httpd.conf", Content: []byte("Listen 80\nIncludeOptional conf.d/*.conf\n")},
		{Path: "/etc/httpd/vhosts.d/site.conf", Content: []byte(vhostConf)},
	}, "/etc/httpd/conf/httpd.conf")
	if !res.Degraded {
		t.Fatal("a captured file that nothing includes must degrade the parse")
	}
	if !strings.Contains(res.DegradedReason, "/etc/httpd/vhosts.d/site.conf") {
		t.Errorf("degraded reason does not name the unreached file: %q", res.DegradedReason)
	}
}
