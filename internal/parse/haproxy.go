package parse

import (
	"fmt"
	"strconv"
	"strings"
)

var haproxyModelled = map[string]string{
	"bind":            ClassMatch,
	"acl":             ClassMatch,
	"use_backend":     ClassProxy,
	"default_backend": ClassProxy,
	"server":          ClassProxy,
	"balance":         ClassProxy,
	"http-request":    ClassHeader,
	"http-response":   ClassHeader,
	"redirect":        ClassRedirect,
	"reqrep":          ClassRewrite,
	"rspadd":          ClassHeader,
	"mode":            ClassOther,
	"option":          ClassOther,
	"timeout":         ClassOther,
	"log":             ClassOther,
	"stats":           ClassOther,
	"maxconn":         ClassOther,
	"capture":         ClassOther,
	"http-check":      ClassOther,
}

type haproxyParser struct {
	src []byte
	res *Result
	ord map[string]int
}

// HAProxy parses a haproxy configuration. HAProxy has no `include`, so the `-f`
// arguments are already the provably complete file set — which is why this
// adapter has the least room for the "incomplete file set" class of error.
// Multiple -f files are concatenated by haproxy itself, so we do the same.
func HAProxy(files []File) *Result {
	p := &haproxyParser{res: &Result{Vendor: "haproxy"}, ord: map[string]int{}}
	if len(files) == 0 {
		p.res.degrade("no haproxy configuration file was captured")
		return p.res
	}

	// Each file is parsed on its own so provenance stays per-file, but sections
	// are accumulated across files in -f order, matching haproxy's own behaviour.
	backends := map[string]*Upstream{}
	var frontends []haproxySection

	for _, f := range files {
		p.src = f.Content
		for _, sec := range p.sections(f.Path, f.Content) {
			switch sec.kind {
			case "frontend", "listen":
				frontends = append(frontends, sec)
				if sec.kind == "listen" {
					// `listen` is a frontend and a backend in one block.
					up := p.backend(sec)
					backends[up.Name] = up
				}
			case "backend":
				up := p.backend(sec)
				backends[up.Name] = up
			default:
				for _, l := range sec.lines {
					p.res.GlobalRules = append(p.res.GlobalRules, p.rule("global", l, f.Path))
				}
			}
		}
	}
	for _, up := range backends {
		p.res.Upstreams = append(p.res.Upstreams, up)
	}
	for _, sec := range frontends {
		p.frontend(sec, backends)
	}
	return p.res
}

// A section is a top-level keyword plus the indented lines under it.
type haproxySection struct {
	kind  string
	name  string
	path  string
	start int
	end   int
	lines []haproxyLine
}

type haproxyLine struct {
	words []string
	start int
	end   int
	raw   string
}

func (l haproxyLine) word(i int) string {
	if i < len(l.words) {
		return l.words[i]
	}
	return ""
}

func (p *haproxyParser) sections(path string, src []byte) []haproxySection {
	var out []haproxySection
	var cur *haproxySection
	pos := 0
	for pos < len(src) {
		lineStart := pos
		lineEnd := pos
		for lineEnd < len(src) && src[lineEnd] != '\n' {
			lineEnd++
		}
		raw := string(src[lineStart:lineEnd])
		pos = lineEnd
		if pos < len(src) {
			pos++
		}
		trimmed := strings.TrimRight(raw, "\r")
		body := strings.TrimSpace(trimmed)
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		if i := strings.Index(body, " #"); i > 0 {
			body = strings.TrimSpace(body[:i])
		}
		words := strings.Fields(body)

		indented := trimmed != "" && (trimmed[0] == ' ' || trimmed[0] == '\t')
		if !indented && isHAProxySectionKeyword(words[0]) {
			if cur != nil {
				cur.end = lineStart
				out = append(out, *cur)
			}
			cur = &haproxySection{
				kind: words[0], path: path, start: lineStart, end: lineEnd,
			}
			if len(words) > 1 {
				cur.name = words[1]
			}
			continue
		}
		if cur == nil {
			continue
		}
		cur.lines = append(cur.lines, haproxyLine{
			words: words, start: lineStart, end: lineEnd, raw: trimmed,
		})
		cur.end = lineEnd
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func isHAProxySectionKeyword(w string) bool {
	switch w {
	case "global", "defaults", "frontend", "backend", "listen", "resolvers",
		"userlist", "peers", "mailers", "ring", "http-errors", "program", "cache":
		return true
	}
	return false
}

// frontend becomes a Site. Its Listeners come from `bind`, its names from any
// host-matching ACL, and its Routes from `use_backend` (rank 90) and
// `default_backend` (rank 99) — in that order, which is the order haproxy
// evaluates them.
func (p *haproxyParser) frontend(sec haproxySection, backends map[string]*Upstream) {
	site := &Site{
		Prov:        Prov{Path: sec.path, Start: sec.start, End: sec.end},
		Kind:        "haproxy_frontend",
		PrimaryName: sec.name,
		Raw:         sec.name,
	}
	// acl name -> the host values it matches, so `use_backend x if is_shop`
	// contributes a Site name rather than an opaque condition.
	aclHosts := map[string][]string{}

	for _, l := range sec.lines {
		switch l.word(0) {
		case "bind":
			site.ListenerKeys = appendUnique(site.ListenerKeys, p.bind(sec, l).NaturalKey)
		case "acl":
			if hosts := hostACL(l.words); len(hosts) > 0 {
				aclHosts[l.word(1)] = hosts
			}
			site.Rules = append(site.Rules, p.rule("site", l, sec.path))
		case "use_backend":
			site.Routes = append(site.Routes, p.useBackend(sec, l, backends, false))
		case "default_backend":
			site.Routes = append(site.Routes, p.useBackend(sec, l, backends, true))
		default:
			site.Rules = append(site.Rules, p.rule("site", l, sec.path))
		}
	}

	// A frontend serves whatever hostnames its host ACLs name. With none, it is a
	// catch-all, which is the honest answer rather than a guess.
	seen := map[string]bool{}
	for _, r := range site.Routes {
		for _, h := range aclHosts[r.Pattern] {
			if !seen[h] {
				seen[h] = true
				site.Names = append(site.Names, SiteName{Name: h, MatchKind: nameKind(h), Ordinal: len(site.Names)})
			}
		}
	}
	for _, hosts := range aclHosts {
		for _, h := range hosts {
			if !seen[h] {
				seen[h] = true
				site.Names = append(site.Names, SiteName{Name: h, MatchKind: nameKind(h), Ordinal: len(site.Names)})
			}
		}
	}
	if len(site.Names) == 0 {
		site.Names = []SiteName{{Name: "", MatchKind: "catch_all"}}
	}
	site.NaturalKey = "frontend:" + sec.name
	site.Ordinal = p.next(site.NaturalKey)
	p.res.Sites = append(p.res.Sites, site)
}

func (p *haproxyParser) bind(sec haproxySection, l haproxyLine) *Listener {
	spec := l.word(1)
	addr, port := "0.0.0.0", 0
	if strings.HasPrefix(spec, "/") {
		addr, port = "unix:"+spec, 0
	} else if h, ps, ok := strings.Cut(spec, ":"); ok {
		addr = h
		if addr == "*" || addr == "" {
			addr = "0.0.0.0"
		}
		port, _ = strconv.Atoi(ps)
	} else if n, err := strconv.Atoi(spec); err == nil {
		port = n
	}

	tls := false
	for i, w := range l.words {
		if w == "ssl" {
			tls = true
		}
		// `crt` on a bind line points at a **combined PEM**: certificate and
		// private key in one file. We record the binding and never read the file.
		if w == "crt" && i+1 < len(l.words) {
			p.res.CertBindings = append(p.res.CertBindings, &CertBinding{
				Prov:        Prov{Path: sec.path, Start: l.start, End: l.end},
				SiteKey:     "frontend:" + sec.name,
				CertPath:    l.words[i+1],
				CombinedPEM: true,
			})
			tls = true
		}
	}

	key := fmt.Sprintf("%s:%d", addr, port)
	protocol := "http"
	if tls {
		key += "+tls"
		protocol = "https"
	}
	for _, existing := range p.res.Listeners {
		if existing.NaturalKey == key {
			return existing
		}
	}
	nl := &Listener{
		Prov:       Prov{Path: sec.path, Start: l.start, End: l.end},
		NaturalKey: key,
		Address:    addr,
		Port:       port,
		TLS:        tls,
		Protocol:   protocol,
		Raw:        l.raw,
	}
	p.res.Listeners = append(p.res.Listeners, nl)
	return nl
}

func (p *haproxyParser) useBackend(sec haproxySection, l haproxyLine, backends map[string]*Upstream, isDefault bool) *Route {
	name := l.word(1)
	matchType, rank := "haproxy_acl_use_backend", RankHAProxyUseBack
	pattern := ""
	if isDefault {
		matchType, rank = "haproxy_default_backend", RankHAProxyDefault
	} else {
		// `use_backend NAME if COND` — the condition is the match, verbatim.
		for i, w := range l.words {
			if w == "if" || w == "unless" {
				pattern = strings.Join(l.words[i+1:], " ")
				break
			}
		}
	}
	r := &Route{
		Prov:           Prov{Path: sec.path, Start: l.start, End: l.end},
		MatchType:      matchType,
		Pattern:        pattern,
		PrecedenceRank: rank,
		Specificity:    len(pattern),
		TargetRaw:      name,
		Raw:            l.raw,
	}
	if _, ok := backends[name]; ok {
		r.UpstreamKey = "backend:" + name
	} else {
		p.res.warn("use_backend references unknown backend " + name)
	}
	r.NaturalKey = "route:" + sec.name + ":" + matchType + ":" + pattern + "->" + name
	r.Ordinal = p.next(r.NaturalKey)
	r.Rules = append(r.Rules, p.rule("route", l, sec.path))
	return r
}

func (p *haproxyParser) backend(sec haproxySection) *Upstream {
	up := &Upstream{
		Prov:       Prov{Path: sec.path, Start: sec.start, End: sec.end},
		NaturalKey: "backend:" + sec.name,
		Name:       sec.name,
		Kind:       "haproxy_backend",
		Raw:        sec.name,
	}
	up.Ordinal = p.next(up.NaturalKey)
	for _, l := range sec.lines {
		switch l.word(0) {
		case "balance":
			up.BalanceMethod = l.word(1)
		case "server":
			host, port := splitHostPort(l.word(2), 80)
			m := &Member{
				Prov:       Prov{Path: sec.path, Start: l.start, End: l.end},
				NaturalKey: up.NaturalKey + "|" + l.word(1),
				Ordinal:    len(up.Members),
				Host:       host,
				Port:       port,
				Scheme:     "http",
				Flags:      strings.Join(l.words[min(3, len(l.words)):], " "),
				Raw:        l.raw,
			}
			for i, w := range l.words {
				if w == "weight" && i+1 < len(l.words) {
					m.Weight, _ = strconv.Atoi(l.words[i+1])
				}
			}
			up.Members = append(up.Members, m)
		}
	}
	return up
}

// hostACL extracts the hostnames from an ACL that matches the Host header. It is
// deliberately narrow: only the forms we can read with certainty count.
func hostACL(words []string) []string {
	if len(words) < 4 {
		return nil
	}
	matcher := words[2]
	if !strings.Contains(matcher, "hdr(host)") && !strings.Contains(matcher, "hdr_dom(host)") &&
		!strings.Contains(matcher, "hdr_beg(host)") && !strings.Contains(matcher, "ssl_fc_sni") {
		return nil
	}
	var out []string
	for _, w := range words[3:] {
		if strings.HasPrefix(w, "-") { // -i, -m str and friends
			continue
		}
		out = append(out, w)
	}
	return out
}

func (p *haproxyParser) rule(scope string, l haproxyLine, path string) *Rule {
	name := l.word(0)
	class, modelled := haproxyModelled[name]
	if !modelled {
		class = ClassOther
	}
	args := strings.Join(l.words[min(1, len(l.words)):], " ")
	key := scope + ":" + name + ":" + args
	return &Rule{
		Prov:        Prov{Path: path, Start: l.start, End: l.end},
		NaturalKey:  key,
		Ordinal:     p.next(key),
		ScopeKind:   scope,
		Directive:   name,
		ActionClass: class,
		Args:        args,
		Raw:         l.raw,
		IsModelled:  modelled,
	}
}

func (p *haproxyParser) next(key string) int {
	n := p.ord[key]
	p.ord[key] = n + 1
	return n
}
