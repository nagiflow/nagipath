package parse

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// nginxModelled is the closed set of directives we claim to understand. Anything
// outside it still becomes a Rule, with is_modelled = 0, so the gap is visible
// instead of invisible.
var nginxModelled = map[string]string{
	"rewrite":              ClassRewrite,
	"try_files":            ClassRewrite,
	"return":               ClassRedirect,
	"error_page":           ClassRedirect,
	"add_header":           ClassHeader,
	"proxy_set_header":     ClassHeader,
	"proxy_hide_header":    ClassHeader,
	"auth_basic":           ClassAuth,
	"auth_basic_user_file": ClassAuth,
	"auth_request":         ClassAuth,
	"proxy_cache":          ClassCache,
	"proxy_cache_valid":    ClassCache,
	"expires":              ClassCache,
	"limit_req":            ClassRateLimit,
	"limit_conn":           ClassRateLimit,
	"limit_rate":           ClassRateLimit,
	"proxy_pass":           ClassProxy,
	"fastcgi_pass":         ClassProxy,
	"uwsgi_pass":           ClassProxy,
	"scgi_pass":            ClassProxy,
	"grpc_pass":            ClassProxy,
	"proxy_redirect":       ClassProxy,
	"allow":                ClassAccessControl,
	"deny":                 ClassAccessControl,
	"satisfy":              ClassAccessControl,
	"internal":             ClassAccessControl,
	"if":                   ClassMatch,
	"root":                 ClassOther,
	"alias":                ClassOther,
	"index":                ClassOther,
	"log_format":           ClassOther,
	"access_log":           ClassOther,
}

type nginxParser struct {
	files  map[string][]byte
	prefix string
	res    *Result
	// upstreamNames lets proxy_pass tell a declared upstream from a bare host.
	upstreamNames map[string]bool
	ord           map[string]int
}

// NGINX parses a captured NGINX file set. main is the path of the root config
// (typically /etc/nginx/nginx.conf); the rest of the set is used to resolve
// `include`. If our resolved file set differs from what `nginx -T` printed, the
// parse is incomplete by definition — the differential test asserts it does not.
func NGINX(files []File, main string) *Result {
	p := &nginxParser{
		files:         make(map[string][]byte, len(files)),
		res:           &Result{Vendor: "nginx"},
		upstreamNames: map[string]bool{},
		ord:           map[string]int{},
	}
	for _, f := range files {
		p.files[f.Path] = f.Content
	}
	if main == "" {
		main = p.guessMain()
	}
	src, ok := p.files[main]
	if !ok {
		p.res.degrade("main configuration file " + main + " was not captured")
		return p.res
	}
	p.prefix = path.Dir(main)

	top, err := lexNginx(main, src)
	if err != nil {
		p.res.degrade(err.Error())
	}
	top = p.expand(top, 0)

	// Two passes: upstream names must be known before proxy_pass is interpreted,
	// and nginx allows an upstream to be declared after the server that uses it.
	p.collectUpstreamNames(top)

	for _, d := range top {
		switch d.Name {
		case "http":
			p.http(d)
		case "stream":
			// Not modelled in v1. Recorded so its absence is visible.
			p.res.warn("stream {} block present at " + d.Path + "; TCP/UDP proxying is not modelled in v1")
			p.res.GlobalRules = append(p.res.GlobalRules, p.rule("global", d, ClassOther, false))
		default:
			p.res.GlobalRules = append(p.res.GlobalRules, p.ruleFor("global", d))
		}
	}
	return p.res
}

func (p *nginxParser) guessMain() string {
	candidates := []string{"/etc/nginx/nginx.conf", "/usr/local/nginx/conf/nginx.conf"}
	for _, c := range candidates {
		if _, ok := p.files[c]; ok {
			return c
		}
	}
	// Fall back to the shallowest path named nginx.conf.
	best := ""
	for path := range p.files {
		if filepath.Base(path) != "nginx.conf" {
			continue
		}
		if best == "" || strings.Count(path, "/") < strings.Count(best, "/") {
			best = path
		}
	}
	return best
}

// ---------------------------------------------------------------- includes

// expand replaces every `include` with the top-level directives of the files it
// matches, in glob order, recursively. Each spliced directive keeps its own Path,
// so provenance points at the included file and not at the including one.
func (p *nginxParser) expand(ds []directive, depth int) []directive {
	if depth > 16 {
		p.res.degrade("include nesting deeper than 16")
		return ds
	}
	out := make([]directive, 0, len(ds))
	for _, d := range ds {
		if d.Name == "include" && len(d.Args) == 1 {
			matches := p.resolveInclude(d.arg(0))
			if len(matches) == 0 {
				// A missing include is a degraded parse with a named reason, not a
				// crash and not a silent omission.
				p.res.degrade(fmt.Sprintf("include %s in %s matched no captured file", d.arg(0), d.Path))
				continue
			}
			for _, m := range matches {
				sub, err := lexNginx(m, p.files[m])
				if err != nil {
					p.res.degrade(err.Error())
				}
				out = append(out, p.expand(sub, depth+1)...)
			}
			continue
		}
		if len(d.Block) > 0 {
			d.Block = p.expand(d.Block, depth+1)
		}
		out = append(out, d)
	}
	return out
}

func (p *nginxParser) resolveInclude(pattern string) []string {
	if !path.IsAbs(pattern) {
		pattern = path.Join(p.prefix, pattern)
	}
	if _, ok := p.files[pattern]; ok && !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}
	}
	var out []string
	for candidate := range p.files {
		if ok, _ := path.Match(pattern, candidate); ok {
			out = append(out, candidate)
		}
	}
	sort.Strings(out) // nginx uses glob(), which sorts; include order is config order
	return out
}

func (p *nginxParser) collectUpstreamNames(ds []directive) {
	for _, d := range ds {
		if d.Name == "upstream" && len(d.Args) > 0 {
			p.upstreamNames[d.arg(0)] = true
		}
		if len(d.Block) > 0 {
			p.collectUpstreamNames(d.Block)
		}
	}
}

// ------------------------------------------------------------------- http

func (p *nginxParser) http(httpDir directive) {
	for _, d := range httpDir.Block {
		switch d.Name {
		case "upstream":
			p.upstream(d)
		case "server":
			p.server(d)
		default:
			p.res.GlobalRules = append(p.res.GlobalRules, p.ruleFor("http", d))
		}
	}
}

func (p *nginxParser) upstream(d directive) {
	name := d.arg(0)
	up := &Upstream{
		Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey: "upstream:" + name,
		Ordinal:    p.next("upstream:" + name),
		Name:       name,
		Kind:       "nginx_upstream",
		Raw:        p.raw(d),
	}
	for _, m := range d.Block {
		switch m.Name {
		case "server":
			host, port := splitHostPort(m.arg(0), 80)
			mem := &Member{
				Prov:       Prov{Path: m.Path, Start: m.Start, End: m.End},
				NaturalKey: up.NaturalKey + "|" + m.arg(0),
				Ordinal:    len(up.Members),
				Host:       host,
				Port:       port,
				Scheme:     "http",
				Flags:      strings.Join(m.Args[min(1, len(m.Args)):], " "),
				Raw:        p.raw(m),
			}
			for _, a := range m.Args[1:] {
				if v, ok := strings.CutPrefix(a, "weight="); ok {
					mem.Weight, _ = strconv.Atoi(v)
				}
			}
			up.Members = append(up.Members, mem)
		case "least_conn", "ip_hash", "random", "hash":
			up.BalanceMethod = m.Name
		}
	}
	p.res.Upstreams = append(p.res.Upstreams, up)
}

// ----------------------------------------------------------------- server

func (p *nginxParser) server(d directive) {
	site := &Site{
		Prov: Prov{Path: d.Path, Start: d.Start, End: d.End},
		Kind: "nginx_server",
		Raw:  p.raw(d),
	}
	var cert, key string
	var certProv Prov

	for _, c := range d.Block {
		switch c.Name {
		case "listen":
			l := p.listen(c)
			site.ListenerKeys = appendUnique(site.ListenerKeys, l.NaturalKey)
		case "server_name":
			for i, n := range c.Args {
				site.Names = append(site.Names, SiteName{Name: n, MatchKind: nameKind(n), Ordinal: i})
			}
		case "root":
			site.DocumentRoot = c.arg(0)
			site.Rules = append(site.Rules, p.ruleFor("site", c))
		case "location":
			site.Routes = append(site.Routes, p.location(c, nil, len(site.Routes)))
		case "ssl_certificate":
			cert = c.arg(0)
			certProv = Prov{Path: c.Path, Start: c.Start, End: c.End}
		case "ssl_certificate_key":
			key = c.arg(0)
		default:
			site.Rules = append(site.Rules, p.ruleFor("site", c))
		}
	}

	if len(site.Names) == 0 {
		site.Names = []SiteName{{Name: "", MatchKind: "catch_all", Ordinal: 0}}
	}
	site.PrimaryName = site.Names[0].Name
	if site.PrimaryName == "" {
		site.PrimaryName = "(default server)"
	}
	site.NaturalKey = "server:" + site.PrimaryName + "@" + strings.Join(site.ListenerKeys, ",")
	site.Ordinal = p.next(site.NaturalKey)

	if cert != "" {
		p.res.CertBindings = append(p.res.CertBindings, &CertBinding{
			Prov: certProv, SiteKey: site.NaturalKey, CertPath: cert, KeyPath: key,
		})
	}
	p.markShadowedHeaders(site)
	p.res.Sites = append(p.res.Sites, site)
}

// listen produces (or reuses) a Listener. Forms handled: `80`, `443 ssl http2`,
// `1.2.3.4:80`, `[::]:80`, `*:8080 default_server`, `unix:/path`.
func (p *nginxParser) listen(d directive) *Listener {
	spec := d.arg(0)
	addr, port := "0.0.0.0", 80
	switch {
	case strings.HasPrefix(spec, "unix:"):
		addr, port = spec, 0
	case strings.HasPrefix(spec, "["):
		if i := strings.LastIndex(spec, "]"); i > 0 {
			addr = spec[1:i]
			if rest, ok := strings.CutPrefix(spec[i+1:], ":"); ok {
				port, _ = strconv.Atoi(rest)
			}
		}
	case strings.Contains(spec, ":"):
		host, ps, _ := strings.Cut(spec, ":")
		addr = host
		if addr == "*" {
			addr = "0.0.0.0"
		}
		port, _ = strconv.Atoi(ps)
	default:
		if n, err := strconv.Atoi(spec); err == nil {
			port = n
		} else {
			addr = spec
		}
	}

	tls := false
	protocol := "http"
	isDefault := false
	for _, a := range d.Args {
		switch {
		case a == "ssl":
			tls, protocol = true, "https"
		case a == "http2":
			protocol = "http2"
		case a == "quic" || a == "http3":
			protocol = "http3"
		case a == "default_server" || a == "default":
			isDefault = true
		}
	}
	if tls && protocol == "http2" {
		protocol = "http2"
	}
	// Port 443 with no `ssl` flag is left as-is on purpose: guessing TLS from a
	// port number is how a plaintext listener gets mislabelled as encrypted.

	key := fmt.Sprintf("%s:%d", addr, port)
	if tls {
		key += "+tls"
	}
	for _, existing := range p.res.Listeners {
		if existing.NaturalKey == key {
			existing.IsDefault = existing.IsDefault || isDefault
			return existing
		}
	}
	l := &Listener{
		Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey: key,
		Ordinal:    0,
		Address:    addr,
		Port:       port,
		TLS:        tls,
		Protocol:   protocol,
		IsDefault:  isDefault,
		Raw:        p.raw(d),
	}
	p.res.Listeners = append(p.res.Listeners, l)
	return l
}

// location builds a Route. The precedence rank encodes NGINX's actual algorithm:
//
//	=      exact           rank 10
//	^~     prefix, no regex rank 20  — a short-circuit, not a longest-match score
//	~ ~*   regex           rank 30  — resolved by FILE ORDER, so specificity is 0
//	       prefix          rank 40  — longest match wins, so specificity = length
//
// Storing 0 specificity for regex is the whole point: a regex implementation that
// ranks by pattern length silently reorders the fleet's real behaviour.
func (p *nginxParser) location(d directive, parent *Route, idx int) *Route {
	modifier, pattern := "", d.arg(0)
	if len(d.Args) >= 2 {
		switch d.arg(0) {
		case "=", "^~", "~", "~*", "@":
			modifier, pattern = d.arg(0), d.arg(1)
		}
	}
	if strings.HasPrefix(pattern, "@") {
		modifier = "@"
	}

	r := &Route{
		Prov:    Prov{Path: d.Path, Start: d.Start, End: d.End},
		Pattern: pattern,
		Ordinal: idx,
		Parent:  parent,
		Raw:     p.raw(d),
	}
	switch modifier {
	case "=":
		r.MatchType, r.PrecedenceRank, r.Specificity = "exact", RankNginxExact, len(pattern)
	case "^~":
		r.MatchType, r.PrecedenceRank, r.Specificity = "prefix_no_regex", RankNginxPrefixNoRE, len(pattern)
	case "~":
		r.MatchType, r.PrecedenceRank, r.Specificity = "regex", RankNginxRegex, 0
	case "~*":
		r.MatchType, r.PrecedenceRank, r.Specificity = "regex_ci", RankNginxRegex, 0
	case "@":
		r.MatchType, r.PrecedenceRank, r.Specificity = "named", RankNginxPrefix, 0
	default:
		r.MatchType, r.PrecedenceRank, r.Specificity = "prefix", RankNginxPrefix, len(pattern)
	}

	prefix := "location:"
	if parent != nil {
		prefix = parent.NaturalKey + "/"
	}
	r.NaturalKey = prefix + r.MatchType + ":" + pattern
	r.Ordinal = p.next(r.NaturalKey)

	for _, c := range d.Block {
		switch c.Name {
		case "location":
			r.Routes = append(r.Routes, p.location(c, r, len(r.Routes)))
		case "proxy_pass", "fastcgi_pass", "uwsgi_pass", "scgi_pass", "grpc_pass":
			p.proxyPass(r, c)
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		case "return":
			r.IsTerminal = true
			r.TargetRaw = c.argsJoined()
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		case "root", "alias":
			r.IsTerminal = true
			if r.TargetRaw == "" {
				r.TargetRaw = c.Name + " " + c.arg(0)
			}
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		case "if":
			// An `if` block's contents belong to the enclosing Route. nginx's `if`
			// is notoriously not a conditional; flattening it keeps the rules
			// visible rather than pretending they form a nested scope.
			r.Rules = append(r.Rules, p.rule("route", c, ClassMatch, true))
			for _, inner := range c.Block {
				r.Rules = append(r.Rules, p.ruleFor("route", inner))
			}
		default:
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		}
	}
	return r
}

// proxyPass resolves the target. A named upstream links to its Upstream row; a
// bare host:port becomes an inline Upstream with one member, so the Trace engine
// has exactly one shape to follow.
func (p *nginxParser) proxyPass(r *Route, d directive) {
	target := d.arg(0)
	r.TargetRaw = target
	if strings.Contains(target, "$") {
		// A variable in proxy_pass means the target is only known at request time.
		p.res.warn("proxy_pass with a variable at " + d.Path + ": target resolved at request time, not traced")
		return
	}

	scheme, rest := "http", target
	if s, r2, ok := strings.Cut(target, "://"); ok {
		scheme, rest = s, r2
	}
	switch d.Name {
	case "fastcgi_pass":
		scheme = "fcgi"
	case "uwsgi_pass":
		scheme = "uwsgi"
	}
	authority := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority = rest[:i]
	}
	if p.upstreamNames[authority] {
		r.UpstreamKey = "upstream:" + authority
		return
	}

	defPort := 80
	if scheme == "https" {
		defPort = 443
	}
	host, port := splitHostPort(authority, defPort)
	key := "inline:" + scheme + "://" + authority
	for _, up := range p.res.Upstreams {
		if up.NaturalKey == key {
			r.UpstreamKey = key
			return
		}
	}
	up := &Upstream{
		Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey: key,
		Ordinal:    p.next(key),
		Name:       authority,
		Kind:       "nginx_inline",
		Raw:        p.raw(d),
		Members: []*Member{{
			Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
			NaturalKey: key + "|" + authority,
			Host:       host,
			Port:       port,
			Scheme:     scheme,
			Raw:        p.raw(d),
		}},
	}
	p.res.Upstreams = append(p.res.Upstreams, up)
	r.UpstreamKey = key
}

// markShadowedHeaders implements NGINX's add_header inheritance: a nearer scope
// with any add_header **discards every inherited one**. It does not merge.
//
// ponytail: this marks a site-scope Rule as shadowed if ANY Route in the Site
// overrides it. The exact per-request answer is recomputed at Trace time against
// the one Route that matched; this flag is what the inventory view needs.
func (p *nginxParser) markShadowedHeaders(site *Site) {
	var overriding []*Route
	var walk func(rs []*Route)
	walk = func(rs []*Route) {
		for _, r := range rs {
			for _, rule := range r.Rules {
				if rule.Directive == "add_header" {
					overriding = append(overriding, r)
					break
				}
			}
			walk(r.Routes)
		}
	}
	walk(site.Routes)
	if len(overriding) == 0 {
		return
	}
	names := make([]string, 0, len(overriding))
	for _, r := range overriding {
		names = append(names, r.Pattern)
	}
	for _, rule := range site.Rules {
		if rule.Directive == "add_header" {
			rule.Shadowed = true
			rule.ShadowedBy = strings.Join(names, ", ")
		}
	}
}

// ------------------------------------------------------------------ rules

func (p *nginxParser) ruleFor(scope string, d directive) *Rule {
	class, modelled := nginxModelled[d.Name]
	if !modelled {
		class = ClassOther
	}
	return p.rule(scope, d, class, modelled)
}

func (p *nginxParser) rule(scope string, d directive, class string, modelled bool) *Rule {
	key := scope + ":" + d.Name + ":" + d.argsJoined()
	return &Rule{
		Prov:        Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey:  key,
		Ordinal:     p.next(key),
		ScopeKind:   scope,
		Directive:   d.Name,
		ActionClass: class,
		Args:        d.argsJoined(),
		Raw:         p.raw(d),
		IsModelled:  modelled,
	}
}

func (p *nginxParser) raw(d directive) string {
	src := p.files[d.Path]
	if d.Start >= 0 && d.End <= len(src) && d.Start < d.End {
		return string(src[d.Start:d.End])
	}
	return d.Name + " " + d.argsJoined()
}

// next assigns the ordinal that disambiguates two rows with an identical natural
// key — the same server block declared twice, for instance.
func (p *nginxParser) next(key string) int {
	n := p.ord[key]
	p.ord[key] = n + 1
	return n
}

// --------------------------------------------------------------- utilities

func splitHostPort(spec string, defPort int) (string, int) {
	spec = strings.TrimSuffix(spec, "/")
	if strings.HasPrefix(spec, "unix:") {
		return spec, 0
	}
	if strings.HasPrefix(spec, "[") {
		if i := strings.LastIndex(spec, "]"); i > 0 {
			host := spec[1:i]
			if rest, ok := strings.CutPrefix(spec[i+1:], ":"); ok {
				if n, err := strconv.Atoi(rest); err == nil {
					return host, n
				}
			}
			return host, defPort
		}
	}
	if host, ps, ok := strings.Cut(spec, ":"); ok {
		if n, err := strconv.Atoi(ps); err == nil {
			return host, n
		}
		return host, defPort
	}
	return spec, defPort
}

func nameKind(n string) string {
	switch {
	case n == "" || n == "_":
		return "catch_all"
	case strings.HasPrefix(n, "~"):
		return "regex"
	case strings.HasPrefix(n, "*."):
		return "wildcard_prefix"
	case strings.HasSuffix(n, ".*"):
		return "wildcard_suffix"
	default:
		return "exact"
	}
}

func appendUnique(xs []string, x string) []string {
	for _, e := range xs {
		if e == x {
			return xs
		}
	}
	return append(xs, x)
}
