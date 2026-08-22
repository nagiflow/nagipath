package parse

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

var apacheModelled = map[string]string{
	"rewriterule":        ClassRewrite,
	"rewritecond":        ClassMatch,
	"rewriteengine":      ClassRewrite,
	"redirect":           ClassRedirect,
	"redirectmatch":      ClassRedirect,
	"redirectpermanent":  ClassRedirect,
	"header":             ClassHeader,
	"requestheader":      ClassHeader,
	"authtype":           ClassAuth,
	"authname":           ClassAuth,
	"authuserfile":       ClassAuth,
	"require":            ClassAccessControl,
	"allow":              ClassAccessControl,
	"deny":               ClassAccessControl,
	"order":              ClassAccessControl,
	"cacheenable":        ClassCache,
	"expiresactive":      ClassCache,
	"expiresdefault":     ClassCache,
	"proxypass":          ClassProxy,
	"proxypassreverse":   ClassProxy,
	"proxypassmatch":     ClassProxy,
	"proxypreservehost":  ClassProxy,
	"proxyset":           ClassProxy,
	"balancermember":     ClassProxy,
	"servername":         ClassMatch,
	"serveralias":        ClassMatch,
	"documentroot":       ClassOther,
	"directoryindex":     ClassOther,
	"errorlog":           ClassOther,
	"customlog":          ClassOther,
	"logformat":          ClassOther,
	"sslengine":          ClassOther,
	"sslcertificatefile": ClassOther,
	"loadmodule":         ClassOther,
	"listen":             ClassOther,
}

type apacheParser struct {
	files map[string][]byte
	root  string // ServerRoot, which is what Apache resolves a relative Include against
	alt   string // the directory httpd.conf is in, which is what it is usually mistaken for
	res   *Result
	ord   map[string]int
}

// Load-time conditionals. Apache decides these when it reads the configuration,
// and everything inside one is ordinary configuration once it does — including
// <VirtualHost> and <Location> sections. Treating the container as one opaque
// directive drops its whole contents, and since real Apache configurations wrap
// most of what they do in <IfModule>, that is most of the configuration.
//
// The condition itself is not evaluated: whether a module is loaded is a fact
// about the running server, not about the text. The container is kept as a Rule
// alongside its hoisted children so the guard stays visible next to what it
// guards.
//
// <If>, <Else> and <ElseIf> are deliberately not here. Those are evaluated per
// request, so hoisting them would turn a branch into a fact.
var apacheConditional = map[string]bool{
	"ifmodule":  true,
	"ifdefine":  true,
	"ifversion": true,
	"iffile":    true,
}

// Apache parses a captured Apache file set. main is the path of httpd.conf (or
// apache2.conf); `Include` and `IncludeOptional` are resolved against the rest of
// the set, with the same "missing include degrades the parse" rule as NGINX.
func Apache(files []File, main string) *Result {
	p := &apacheParser{
		files: make(map[string][]byte, len(files)),
		res:   &Result{Vendor: "apache"},
		ord:   map[string]int{},
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
	top, err := lexApache(main, src)
	if err != nil {
		p.res.degrade(err.Error())
	}

	// A relative Include is resolved against ServerRoot, not against the directory
	// httpd.conf lives in. On the stock httpd image those are /usr/local/apache2
	// and /usr/local/apache2/conf, so getting it wrong finds nothing — and with
	// IncludeOptional, finding nothing is not an error on either side. Both are
	// tried, and resolve() says so when only the wrong one matched.
	p.alt = path.Dir(main)
	p.root = p.alt
	for _, d := range top {
		if lower(d.Name) == "serverroot" && d.arg(0) != "" {
			p.root = strings.TrimSuffix(d.arg(0), "/")
		}
	}

	top = p.expand(top, 0)

	for _, d := range top {
		switch lower(d.Name) {
		case "virtualhost":
			p.vhost(d)
		case "listen":
			p.listen(d)
		case "proxy":
			p.balancer(d)
		default:
			p.res.GlobalRules = append(p.res.GlobalRules, p.ruleFor("global", d))
		}
	}
	return p.res
}

func (p *apacheParser) guessMain() string {
	for _, c := range []string{
		"/etc/httpd/conf/httpd.conf",
		"/etc/apache2/apache2.conf",
		"/usr/local/apache2/conf/httpd.conf",
	} {
		if _, ok := p.files[c]; ok {
			return c
		}
	}
	best := ""
	for f := range p.files {
		base := path.Base(f)
		if base != "httpd.conf" && base != "apache2.conf" {
			continue
		}
		if best == "" || strings.Count(f, "/") < strings.Count(best, "/") {
			best = f
		}
	}
	return best
}

func (p *apacheParser) expand(ds []directive, depth int) []directive {
	if depth > 16 {
		p.res.degrade("Include nesting deeper than 16")
		return ds
	}
	out := make([]directive, 0, len(ds))
	for _, d := range ds {
		name := lower(d.Name)
		if name == "include" || name == "includeoptional" {
			matches := p.resolve(d.arg(0))
			if len(matches) == 0 {
				if name == "include" {
					p.res.degrade(fmt.Sprintf("Include %s in %s matched no captured file", d.arg(0), d.Path))
				}
				continue
			}
			for _, m := range matches {
				sub, err := lexApache(m, p.files[m])
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
		if apacheConditional[name] {
			// The guard first, then its contents at the level Apache would put them.
			guard := d
			guard.Block = nil
			out = append(out, guard)
			out = append(out, d.Block...)
			continue
		}
		out = append(out, d)
	}
	return out
}

func (p *apacheParser) resolve(pattern string) []string {
	if pattern == "" {
		return nil
	}
	if path.IsAbs(pattern) {
		return p.match(pattern)
	}
	// ServerRoot is what Apache uses, so it wins. The config file's own directory
	// is tried only as a fallback, and only to report the discrepancy rather than
	// to paper over it: a tree that is reachable one way and not the other is a
	// tree the running server is not loading.
	if out := p.match(path.Join(p.root, pattern)); len(out) > 0 {
		return out
	}
	if out := p.match(path.Join(p.alt, pattern)); len(out) > 0 && p.alt != p.root {
		p.res.degrade(fmt.Sprintf(
			"Include %s matched files under %s but nothing under ServerRoot %s, "+
				"so Apache resolves it to nothing", pattern, p.alt, p.root))
		return out
	}
	return nil
}

func (p *apacheParser) match(pattern string) []string {
	if _, ok := p.files[pattern]; ok && !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}
	}
	var out []string
	for f := range p.files {
		if ok, _ := path.Match(pattern, f); ok {
			out = append(out, f)
			continue
		}
		// A bare directory include pulls in every file beneath it.
		if !strings.ContainsAny(pattern, "*?[") && strings.HasPrefix(f, pattern+"/") {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func (p *apacheParser) listen(d directive) *Listener {
	addr, port := "0.0.0.0", 80
	spec := d.arg(0)
	if strings.Contains(spec, ":") {
		h, ps, _ := strings.Cut(spec, ":")
		addr = h
		port, _ = strconv.Atoi(ps)
	} else if n, err := strconv.Atoi(spec); err == nil {
		port = n
	}
	tls := lower(d.arg(1)) == "https"
	return p.listener(d, addr, port, tls)
}

func (p *apacheParser) listener(d directive, addr string, port int, tls bool) *Listener {
	if addr == "*" || addr == "" {
		addr = "0.0.0.0"
	}
	key := fmt.Sprintf("%s:%d", addr, port)
	protocol := "http"
	if tls {
		key += "+tls"
		protocol = "https"
	}
	for _, l := range p.res.Listeners {
		if l.NaturalKey == key {
			return l
		}
	}
	l := &Listener{
		Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey: key,
		Address:    addr,
		Port:       port,
		TLS:        tls,
		Protocol:   protocol,
		Raw:        p.raw(d),
	}
	p.res.Listeners = append(p.res.Listeners, l)
	return l
}

// --------------------------------------------------------------- virtualhost

func (p *apacheParser) vhost(d directive) {
	site := &Site{
		Prov: Prov{Path: d.Path, Start: d.Start, End: d.End},
		Kind: "apache_vhost",
		Raw:  p.raw(d),
	}
	// TLS is decided by SSLEngine / SSLCertificateFile, not by the port number.
	// Inferring https from :443 is how a plaintext vhost gets mislabelled.
	tls := false
	for _, c := range d.Block {
		switch lower(c.Name) {
		case "sslengine":
			tls = tls || lower(c.arg(0)) == "on"
		case "sslcertificatefile":
			tls = true
		}
	}

	addrs := d.Args
	if len(addrs) == 0 {
		addrs = []string{"*:80"}
	}
	for _, spec := range addrs {
		host, ps, ok := strings.Cut(spec, ":")
		port := 80
		if ok {
			port, _ = strconv.Atoi(ps)
		} else {
			host = spec
		}
		l := p.listener(d, host, port, tls)
		site.ListenerKeys = appendUnique(site.ListenerKeys, l.NaturalKey)
	}

	var cert, key, chain string
	var certProv Prov

	for _, c := range d.Block {
		switch lower(c.Name) {
		case "servername":
			site.Names = append([]SiteName{{Name: stripScheme(c.arg(0)), MatchKind: nameKind(stripScheme(c.arg(0)))}}, site.Names...)
		case "serveralias":
			for _, a := range c.Args {
				site.Names = append(site.Names, SiteName{Name: a, MatchKind: nameKind(a), Ordinal: len(site.Names)})
			}
		case "documentroot":
			site.DocumentRoot = c.arg(0)
			site.Rules = append(site.Rules, p.ruleFor("site", c))
		case "directory", "directorymatch", "location", "locationmatch", "files", "filesmatch":
			site.Routes = append(site.Routes, p.section(c, nil))
		case "proxypass", "proxypassmatch":
			// ProxyPass at vhost scope is a routing statement, not a plain rule:
			// it matches a path prefix exactly like <Location> does.
			site.Routes = append(site.Routes, p.proxyPassRoute(c))
		case "sslcertificatefile":
			cert = c.arg(0)
			certProv = Prov{Path: c.Path, Start: c.Start, End: c.End}
		case "sslcertificatekeyfile":
			key = c.arg(0)
		case "sslcertificatechainfile":
			chain = c.arg(0)
		case "proxy":
			p.balancer(c)
		default:
			site.Rules = append(site.Rules, p.ruleFor("site", c))
		}
	}

	if len(site.Names) == 0 {
		site.Names = []SiteName{{Name: "", MatchKind: "catch_all"}}
	}
	for i := range site.Names {
		site.Names[i].Ordinal = i
	}
	site.PrimaryName = site.Names[0].Name
	if site.PrimaryName == "" {
		site.PrimaryName = "(default vhost)"
	}
	site.NaturalKey = "vhost:" + site.PrimaryName + "@" + strings.Join(site.ListenerKeys, ",")
	site.Ordinal = p.next(site.NaturalKey)

	if cert != "" {
		p.res.CertBindings = append(p.res.CertBindings, &CertBinding{
			Prov: certProv, SiteKey: site.NaturalKey,
			CertPath: cert, KeyPath: key, ChainPath: chain,
		})
	}
	p.res.Sites = append(p.res.Sites, site)
}

// section builds a Route from a container directive. The ranks encode Apache's
// merge order: Directory, then DirectoryMatch, then Files, then Location — so
// **<Location> beats <Directory>**. Reversing these is the Apache equivalent of
// getting NGINX regex ordering backwards.
func (p *apacheParser) section(d directive, parent *Route) *Route {
	pattern := d.arg(0)
	var matchType string
	var rank int
	switch lower(d.Name) {
	case "directory":
		matchType, rank = "directory", RankApacheDirectory
	case "directorymatch":
		matchType, rank = "directory_match", RankApacheDirMatch
	case "files":
		matchType, rank = "files", RankApacheFiles
	case "filesmatch":
		matchType, rank = "files_match", RankApacheFiles
	case "location":
		matchType, rank = "location", RankApacheLocation
	case "locationmatch":
		matchType, rank = "location_match", RankApacheLocation
	}

	r := &Route{
		Prov:           Prov{Path: d.Path, Start: d.Start, End: d.End},
		MatchType:      matchType,
		Pattern:        pattern,
		PrecedenceRank: rank,
		Specificity:    len(pattern),
		Parent:         parent,
		Raw:            p.raw(d),
	}
	if strings.HasSuffix(matchType, "_match") {
		r.Specificity = 0 // regex containers are resolved by config order
	}
	prefix := "section:"
	if parent != nil {
		prefix = parent.NaturalKey + "/"
	}
	r.NaturalKey = prefix + matchType + ":" + pattern
	r.Ordinal = p.next(r.NaturalKey)

	for _, c := range d.Block {
		switch lower(c.Name) {
		case "directory", "directorymatch", "location", "locationmatch", "files", "filesmatch":
			r.Routes = append(r.Routes, p.section(c, r))
		case "proxypass", "proxypassmatch":
			p.attachProxy(r, c, c.arg(0))
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		case "rewriterule":
			if isLast(c.Args) {
				r.IsTerminal = true
			}
			if r.TargetRaw == "" {
				r.TargetRaw = c.argsJoined()
			}
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		default:
			r.Rules = append(r.Rules, p.ruleFor("route", c))
		}
	}
	return r
}

// proxyPassRoute turns `ProxyPass /path http://target/` into a Route so the Trace
// engine sees one shape for "this path goes elsewhere".
func (p *apacheParser) proxyPassRoute(d directive) *Route {
	pattern, target := d.arg(0), d.arg(1)
	if len(d.Args) == 1 { // ProxyPass inside a <Location>: only the target is given
		pattern, target = "/", d.arg(0)
	}
	matchType, rank := "location", RankApacheLocation
	if lower(d.Name) == "proxypassmatch" {
		matchType = "location_match"
	}
	r := &Route{
		Prov:           Prov{Path: d.Path, Start: d.Start, End: d.End},
		MatchType:      matchType,
		Pattern:        pattern,
		PrecedenceRank: rank,
		Specificity:    len(pattern),
		Raw:            p.raw(d),
	}
	r.NaturalKey = "proxypass:" + pattern
	r.Ordinal = p.next(r.NaturalKey)
	r.Rules = append(r.Rules, p.ruleFor("route", d))
	p.attachProxy(r, d, target)
	return r
}

func (p *apacheParser) attachProxy(r *Route, d directive, target string) {
	if target == "" || target == "!" {
		return
	}
	if len(d.Args) >= 2 && strings.HasPrefix(d.arg(1), "http") {
		target = d.arg(1)
	}
	r.TargetRaw = target

	if rest, ok := strings.CutPrefix(target, "balancer://"); ok {
		name, _, _ := strings.Cut(rest, "/")
		r.UpstreamKey = "balancer:" + name
		return
	}
	// `unix:/run/php/php-fpm.sock|fcgi://localhost/srv/app/$1` — the socket is the
	// destination and the URL after the pipe only supplies the scheme and a
	// placeholder authority. Recording `localhost` here would name a host that is
	// not where the request goes.
	socket := ""
	if rest, ok := strings.CutPrefix(target, "unix:"); ok {
		socket, target, _ = strings.Cut(rest, "|")
	}

	scheme, rest := "http", target
	if s, r2, ok := strings.Cut(target, "://"); ok {
		scheme, rest = s, r2
	}
	// mod_proxy_wstunnel's schemes are the HTTP ones with an Upgrade; the transport
	// and the TLS question are the same, and ws/wss are not distinct destinations.
	switch scheme {
	case "ws":
		scheme = "http"
	case "wss":
		scheme = "https"
	}
	authority, _, _ := strings.Cut(rest, "/")
	defPort := 80
	if scheme == "https" {
		defPort = 443
	}
	host, port := splitHostPort(authority, defPort)
	if socket != "" {
		host, port, authority = socket, 0, socket
	}
	key := "inline:" + scheme + "://" + authority
	for _, up := range p.res.Upstreams {
		if up.NaturalKey == key {
			r.UpstreamKey = key
			return
		}
	}
	p.res.Upstreams = append(p.res.Upstreams, &Upstream{
		Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey: key,
		Ordinal:    p.next(key),
		Name:       authority,
		Kind:       "apache_inline",
		Raw:        p.raw(d),
		Members: []*Member{{
			Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
			NaturalKey: key + "|" + authority,
			Host:       host,
			Port:       port,
			Scheme:     scheme,
			Raw:        p.raw(d),
		}},
	})
	r.UpstreamKey = key
}

// balancer handles <Proxy balancer://name> … BalancerMember …
func (p *apacheParser) balancer(d directive) {
	spec := d.arg(0)
	rest, ok := strings.CutPrefix(spec, "balancer://")
	if !ok {
		p.res.GlobalRules = append(p.res.GlobalRules, p.ruleFor("global", d))
		return
	}
	name, _, _ := strings.Cut(rest, "/")
	up := &Upstream{
		Prov:       Prov{Path: d.Path, Start: d.Start, End: d.End},
		NaturalKey: "balancer:" + name,
		Name:       name,
		Kind:       "apache_balancer",
		Raw:        p.raw(d),
	}
	up.Ordinal = p.next(up.NaturalKey)
	for _, c := range d.Block {
		switch lower(c.Name) {
		case "balancermember":
			target := c.arg(0)
			scheme, authority := "http", target
			if s, r2, ok := strings.Cut(target, "://"); ok {
				scheme, authority = s, r2
			}
			if i := strings.IndexByte(authority, '/'); i >= 0 {
				authority = authority[:i]
			}
			defPort := 80
			if scheme == "https" {
				defPort = 443
			}
			host, port := splitHostPort(authority, defPort)
			m := &Member{
				Prov:       Prov{Path: c.Path, Start: c.Start, End: c.End},
				NaturalKey: up.NaturalKey + "|" + authority,
				Ordinal:    len(up.Members),
				Host:       host,
				Port:       port,
				Scheme:     scheme,
				Flags:      strings.Join(c.Args[min(1, len(c.Args)):], " "),
				Raw:        p.raw(c),
			}
			for _, a := range c.Args[1:] {
				if v, ok := strings.CutPrefix(a, "loadfactor="); ok {
					m.Weight, _ = strconv.Atoi(v)
				}
			}
			up.Members = append(up.Members, m)
		case "proxyset":
			for _, a := range c.Args {
				if v, ok := strings.CutPrefix(a, "lbmethod="); ok {
					up.BalanceMethod = v
				}
			}
		}
	}
	p.res.Upstreams = append(p.res.Upstreams, up)
}

// ------------------------------------------------------------------- rules

func (p *apacheParser) ruleFor(scope string, d directive) *Rule {
	class, modelled := apacheModelled[lower(d.Name)]
	if !modelled {
		class = ClassOther
	}
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

func (p *apacheParser) raw(d directive) string {
	src := p.files[d.Path]
	if d.Start >= 0 && d.End <= len(src) && d.Start < d.End {
		return string(src[d.Start:d.End])
	}
	return d.Name + " " + d.argsJoined()
}

func (p *apacheParser) next(key string) int {
	n := p.ord[key]
	p.ord[key] = n + 1
	return n
}

func lower(s string) string { return strings.ToLower(s) }

func stripScheme(s string) string {
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	host, _, _ := strings.Cut(s, ":")
	return host
}

// isLast reports whether a RewriteRule carries the [L] flag, which stops the
// rewrite chain.
func isLast(args []string) bool {
	if len(args) == 0 {
		return false
	}
	flags := args[len(args)-1]
	if !strings.HasPrefix(flags, "[") {
		return false
	}
	for _, f := range strings.Split(strings.Trim(flags, "[]"), ",") {
		if f == "L" || f == "last" || f == "END" || f == "end" {
			return true
		}
	}
	return false
}
