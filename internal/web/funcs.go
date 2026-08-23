package web

import (
	"fmt"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

var funcs = template.FuncMap{
	"bytes":         humanBytes,
	"join":          join,
	"reason":        reasonText,
	"sudoers":       func() string { return sudoersGrant },
	"expiry":        expiry,
	"uncovered":     uncovered,
	"on":            navOn,
	"windows":       certWindows,
	"has":           has,
	"csv":           csv,
	"bar":           bar,
	"hl":            highlight,
	"routing":       routingRules,
	"levels":        hopLevels,
	"hoplabels":     hopLabels,
	"stillinferred": stillInferred,
	"probed":        probed,
	"globals":       globalRules,
	"nexturl":       nextURL,
	"reasons":       endingReasons,
	"sitekind":      siteKind,
	"matched":       matchedBy,
	"routekind":     routeKind,
	"toolbar":       toolbar,
	"card":          card,
	"cardif":        cardif,
	"cards":         cards,
	"warnif":        warnif,
	"cols":          cols,
	"add":           func(a, b int) int { return a + b },
	"sub":           func(a, b int) int { return a - b },
	"notglobal":     notGlobal,
	"globalonly":    globalOnly,
}

// hopLevel is one rank of the trace: every instance the request could be at
// after the same number of proxy steps.
type hopLevel struct {
	Hops   []*trace.Hop
	Groups []hopGroup
}

// hopGroup is the hops of one rank that share a cluster. Members of a cluster run
// byte-identical configuration, so the graph sets them side by side under the
// cluster's name: that is one answer on two hosts, not two answers. Hops in no
// cluster are a group of one, which keeps the template to a single loop.
type hopGroup struct {
	Cluster string
	Hops    []*trace.Hop
}

func clusterGroups(hops []*trace.Hop) []hopGroup {
	var out []hopGroup
	at := map[int64]int{}
	for _, h := range hops {
		id := int64(0)
		if h.Inst != nil {
			id = h.Inst.ClusterID
		}
		if i, ok := at[id]; ok && id != 0 {
			out[i].Hops = append(out[i].Hops, h)
			continue
		}
		g := hopGroup{Hops: []*trace.Hop{h}}
		if id != 0 {
			g.Cluster = h.Inst.ClusterName
			at[id] = len(out)
		}
		out = append(out, g)
	}
	return out
}

// hopLevels arranges the hops by distance from the entry. Hops are a tree, not a
// chain: two members of one upstream are alternatives the balancer picks between,
// and stacking them with an arrow in between claims the request visits both.
func hopLevels(hops []*trace.Hop) []hopLevel {
	// Parents are always appended before their children, so one pass is enough.
	depth := make([]int, len(hops))
	var out []hopLevel
	for i, h := range hops {
		d := 0
		if h.ArrivedFrom >= 0 && h.ArrivedFrom < i {
			d = depth[h.ArrivedFrom] + 1
		}
		depth[i] = d
		for len(out) <= d {
			out = append(out, hopLevel{})
		}
		out[d].Hops = append(out[d].Hops, h)
	}
	for i := range out {
		out[i].Groups = clusterGroups(out[i].Hops)
	}
	return out
}

// hopLabels names each hop by the rank it sits on, with a letter when that rank
// holds alternatives: 0, then 1-a and 1-b. Numbering them 0, 1, 2 reads as three
// steps in sequence, which is the opposite of what a balancer does — the request
// reaches exactly one of 1-a and 1-b. Keyed by hop ordinal, which is its index.
func hopLabels(hops []*trace.Hop) map[int]string {
	depth := make([]int, len(hops))
	rank := map[int][]int{}
	for i, h := range hops {
		d := 0
		if h.ArrivedFrom >= 0 && h.ArrivedFrom < i {
			d = depth[h.ArrivedFrom] + 1
		}
		depth[i] = d
		rank[d] = append(rank[d], i)
	}
	out := make(map[int]string, len(hops))
	for d, at := range rank {
		for j, i := range at {
			out[i] = strconv.Itoa(d)
			if len(at) > 1 {
				out[i] += "-" + suffix(j)
			}
		}
	}
	return out
}

// probedHop is what one stored Probe proved about one hop, in the words the live run
// used. Several rules on a hop is several evidence rows and one fact, so the rows are
// folded onto the hop. Built from the trace's hops rather than from the rows alone: a
// hop the probe could not read at all belongs in the list saying so, not quietly
// missing from it.
type probedHop struct {
	Ordinal  int
	Label    string // the host, because that is how an operator names a hop
	Instance string
	Evidence string
	LogPath  string
	Before   string
	After    string
	// sawRow distinguishes a hop the probe examined and could not raise from one it
	// never got a reading on at all. Both stay inferred; only one of them is the
	// configuration's fault.
	sawRow bool
}

// probed matches rows to hops by ordinal and instance both, the way the green ring is
// matched: hop ordinals are only stable while the configuration is, and a verified row
// shown against the wrong hop is exactly the confident wrong Verified this refuses.
func probed(hops []*trace.Hop, last *probe.Past) []probedHop {
	if last == nil {
		return nil
	}
	var out []probedHop
	for _, h := range hops {
		if h.IsExternal || h.Inst == nil {
			continue
		}
		p := probedHop{Ordinal: h.Ordinal, Label: instLabel(h.Inst),
			Instance: h.Inst.DisplayName, Before: "inferred", After: "inferred"}
		// logNote is what the log-correlation attempt itself found, kept separate from
		// p.Evidence so a header result — the stronger of the two when both exist — is
		// never overwritten by it, while still beating the "nothing was read" default
		// below when it is all there is.
		var logNote string
		for _, e := range last.Evidence {
			if e.HopOrdinal != h.Ordinal || e.Instance != p.Instance {
				continue
			}
			p.sawRow = true
			if e.Prior != "" {
				p.Before = e.Prior
			}
			switch {
			case e.Grants == "verified":
				p.After, p.LogPath = "verified", e.LogPath
				p.Evidence = "access log line carrying the correlation token"
			case e.Grants == "observed_effect" && p.After != "verified":
				p.After = "observed_effect"
				p.Evidence = "response header this hop declares"
			case e.Kind == "access_log_absent":
				logNote = e.Raw
			case e.Kind == "access_log_error":
				logNote = "could not read its access log — " + e.Raw
			}
		}
		if p.Evidence == "" {
			// Only what the stored rows support. A header_absent row proves the response
			// carried nothing this hop declares; it says nothing about the access log.
			switch {
			case logNote != "":
				p.Evidence = logNote
			case p.sawRow:
				p.Evidence = "no header this hop declares came back"
			default:
				p.Evidence = "nagipath read nothing from this hop"
			}
		}
		out = append(out, p)
	}
	return out
}

// stillInferred names the hops a probe could not raise. A screen that reports only its
// successes is the confident wrong Verified this product refuses, so the honest
// outcome gets a sentence of its own.
func stillInferred(hops []probedHop) []string {
	var out []string
	for _, h := range hops {
		if h.After == "inferred" {
			out = append(out, h.Label)
		}
	}
	return out
}

// instLabel is the host a hop runs on, falling back to the instance name when the
// node is unknown.
func instLabel(i *trace.Inst) string {
	if i.NodeName != "" {
		return i.NodeName
	}
	return i.DisplayName
}

// suffix is a, b, c … then a1, a2 past the alphabet. An upstream with 27 members on
// one rank is not a graph anyone reads, but it must not collide.
func suffix(j int) string {
	if j < 26 {
		return string(rune('a' + j))
	}
	return "a" + strconv.Itoa(j-25)
}

// ruleFile is one config file's contribution to a hop.
type ruleFile struct {
	Path    string
	Rules   []trace.HopRule
	Classes string // the distinct action classes inside, for the collapsed summary
}

// Has reports whether the selected rule is in this file, so the group holding it
// renders open and a ?rule= link is not a click into a closed box.
func (f ruleFile) Has(id int64) bool {
	for _, hr := range f.Rules {
		if hr.Rule.ID == id {
			return true
		}
	}
	return false
}

// routingRules keeps the rules that decided this hop and groups them by the file
// they came from, in first-seen order. Global scope is dropped: it is the same
// hundred directives on every hop, it decides nothing about routing, and it is
// already on the instance page.
func routingRules(rules []trace.HopRule) []ruleFile {
	var out []ruleFile
	at := map[string]int{}
	for _, hr := range rules {
		if hr.Scope == "global" {
			continue
		}
		i, ok := at[hr.Rule.Path]
		if !ok {
			i = len(out)
			at[hr.Rule.Path] = i
			out = append(out, ruleFile{Path: hr.Rule.Path})
		}
		out[i].Rules = append(out[i].Rules, hr)
		if c := hr.Rule.ActionClass; c != "" && !strings.Contains(out[i].Classes, c) {
			if out[i].Classes != "" {
				out[i].Classes += ", "
			}
			out[i].Classes += c
		}
	}
	return out
}

// siteKind names the kind of configuration block a Site is, in the vendor's own
// words. Without it a name like `fe_public` is unreadable: it is a haproxy
// frontend, not a hostname, and the screen has to say which.
func siteKind(kind string) string {
	switch kind {
	case "haproxy_frontend":
		return "frontend"
	case "haproxy_backend":
		return "backend"
	case "nginx_server":
		return "server block"
	case "apache_vhost":
		return "vhost"
	}
	return kind
}

// routeKind is a route match type in words: `haproxy_default_backend` is a
// database value, "default backend" is what it means.
func routeKind(matchType string) string {
	return strings.ReplaceAll(strings.TrimPrefix(matchType, "haproxy_"), "_", " ")
}

// matchedBy is the match phrase without the hostname the page already prints
// beside it: "exact name shop.example.com" next to shop.example.com is one fact
// twice. A catch-all, a wildcard or a regex keeps its own words.
func matchedBy(phrase, hostname string) string {
	return strings.TrimSpace(strings.TrimSuffix(phrase, hostname))
}

// endingReasons is every distinct sentence explaining how a branch ended, in
// hop order. Distinct, because two branches ending the same way is one fact.
func endingReasons(hops []*trace.Hop) []string {
	var out []string
	seen := map[string]bool{}
	for _, h := range hops {
		if h.Terminal == "" || h.ExternalReason == "" || seen[h.ExternalReason] {
			continue
		}
		seen[h.ExternalReason] = true
		out = append(out, h.ExternalReason)
	}
	return out
}

func globalRules(rules []trace.HopRule) []trace.HopRule {
	var out []trace.HopRule
	for _, hr := range rules {
		if hr.Scope == "global" {
			out = append(out, hr)
		}
	}
	return out
}

// notGlobal and globalOnly are routingRules/globalRules for Rule Lookup's flat,
// ordinal-ordered table: the same "global settings decide nothing about routing,
// and it is the same hundred directives on every result" reasoning, but keeping
// LookupRule's flat order rather than grouping by file, because the ordinal here
// is the evaluation order the operator is reading the table for. A real haproxy
// or nginx global section runs to dozens of tuning directives that apply to
// every request identically; burying the handful of rules that actually decide
// this one under all of them is the opposite of what a lookup is for.
func notGlobal(rules []trace.LookupRule) []trace.LookupRule {
	var out []trace.LookupRule
	for _, r := range rules {
		if r.Scope != "global" {
			out = append(out, r)
		}
	}
	return out
}

func globalOnly(rules []trace.LookupRule) []trace.LookupRule {
	var out []trace.LookupRule
	for _, r := range rules {
		if r.Scope == "global" {
			out = append(out, r)
		}
	}
	return out
}

// nextURL is the request the hop makes to one upstream member: the path after
// this hop's rewrites, on the member's own scheme and port. The Host header is
// unchanged, which is why the template prints it beside this.
func nextURL(n trace.Next, path string) string {
	scheme := n.Member.Scheme
	if scheme == "" {
		// haproxy says TLS with a server flag rather than a scheme, and calling an
		// ssl backend http:// would be wrong in the direction that matters.
		scheme = "http"
		if strings.Contains(n.Member.Flags, "ssl") {
			scheme = "https"
		}
	}
	port := n.Member.Port
	if port == 0 {
		if scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, n.Member.Host, port, path)
}

// has reports whether a filter is currently selected, so a checkbox can render
// checked. An empty filter list means "everything", which is not the same as
// every box being ticked, and the templates say so in words.
func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func csv(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// bar is the height of one column in the collection-activity strip, as a
// percentage. It is a bar chart made of two divs: the design system rules out
// charts, and this is the one place a shape reads faster than a number, so it
// stays a shape with the number printed beside it.
func bar(n, max int) int {
	if max <= 0 || n <= 0 {
		return 0
	}
	if h := n * 100 / max; h > 4 {
		return h
	}
	return 4
}

// highlight marks the query inside a snippet. Everything is escaped first and
// only the <mark> is added back, so a config file containing HTML cannot inject
// anything.
func highlight(snippet, query string) template.HTML {
	esc := template.HTMLEscapeString(snippet)
	q := strings.TrimSpace(query)
	if q == "" {
		return template.HTML(esc)
	}
	var b strings.Builder
	// Case-insensitive scan over the escaped text: the terms an operator searches
	// for (directive names, hostnames) never contain the characters escaping
	// changes, so matching after escaping is safe here.
	low, qlow := strings.ToLower(esc), strings.ToLower(template.HTMLEscapeString(q))
	for {
		i := strings.Index(low, qlow)
		if i < 0 || qlow == "" {
			b.WriteString(esc)
			break
		}
		b.WriteString(esc[:i])
		b.WriteString("<mark>")
		b.WriteString(esc[i : i+len(qlow)])
		b.WriteString("</mark>")
		esc, low = esc[i+len(qlow):], low[i+len(qlow):]
	}
	return template.HTML(b.String())
}

// certWindows counts the certificate list into the expiry buckets the page leads
// with. Plain counts, no score and no grade: the operator reads the finding, not
// a number invented to summarise it.
func certWindows(list []store.CertificateView) []Card {
	var expired, d7, d30, d90 int
	for _, c := range list {
		t, err := time.Parse(time.RFC3339, c.NotAfter)
		if err != nil {
			continue
		}
		days := int(time.Until(t).Hours() / 24)
		switch {
		case days < 0:
			expired++
		case days <= 7:
			d7++
		case days <= 30:
			d30++
		case days <= 90:
			d90++
		}
	}
	return []Card{
		// Soonest first: the list is read top-left to bottom-right, and what is
		// already expired is history next to what expires this week.
		{N: d7, Label: "≤ 7 days", Tone: warnif(d7)},
		{N: d30, Label: "≤ 30 days", Tone: warnif(d30)},
		{N: d90, Label: "≤ 90 days"},
		{N: expired, Label: "expired", Tone: warnif(expired)},
	}
}

// navOn marks the nav item that owns the current path. "/nodes" stays lit on
// "/nodes/7" because the detail page is that section, not a section of its own,
// and one item can own several paths: Fleet covers nodes and instances both.
func navOn(path string, sections ...string) string {
	for _, section := range sections {
		if path == section || (section != "/" && strings.HasPrefix(path, section+"/")) {
			return "on"
		}
	}
	return ""
}

// expiry turns a notAfter timestamp into the phrase an operator reads first. The
// date alone makes every reader do arithmetic before they can tell an emergency
// from a formality.
func expiry(notAfter string) string {
	t, err := time.Parse(time.RFC3339, notAfter)
	if err != nil {
		return ""
	}
	days := int(time.Until(t).Hours() / 24)
	switch {
	case days < 0:
		return fmt.Sprintf("expired %d days ago", -days)
	case days == 0:
		return "expires today"
	case days == 1:
		return "expires tomorrow"
	default:
		return fmt.Sprintf("%d days left", days)
	}
}

// uncovered lists the hostnames a certificate is serving but does not identify.
// This is metadata arithmetic, not a judgement: the certificate says which names
// it covers, the configuration says which it was bound to, and any name in the
// second set and not the first fails in every browser.
func uncovered(cn string, sans []string, serves []string) []string {
	names := sans
	if cn != "" {
		names = append(append([]string{}, sans...), cn)
	}
	var bad []string
	for _, s := range serves {
		s = strings.TrimSpace(s)
		if s == "" || s == "_" || s == "*" {
			continue
		}
		if !coveredBy(s, names) {
			bad = append(bad, s)
		}
	}
	return bad
}

func coveredBy(name string, names []string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(n, ".")))
		if n == name {
			return true
		}
		// A wildcard matches exactly one label, so *.example.com covers
		// a.example.com but neither example.com nor a.b.example.com (RFC 6125).
		if suffix, ok := strings.CutPrefix(n, "*."); ok {
			if host, _, found := strings.Cut(name, "."); found && host != "" &&
				strings.TrimPrefix(name, host+".") == suffix {
				return true
			}
		}
	}
	return false
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	}
}

func join(v any) string {
	switch s := v.(type) {
	case []string:
		return strings.Join(s, ", ")
	case []int64:
		parts := make([]string, len(s))
		for i, n := range s {
			parts[i] = strconv.FormatInt(n, 10)
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

// reasonText turns a terminal reason into the sentence an operator can act on.
// The enum values are stable identifiers; these are the words.
func reasonText(code string) string {
	switch code {
	case "external_hop":
		return "the request leaves the fleet"
	case "no_matching_listener":
		return "nothing is listening there"
	case "no_matching_site":
		return "something listens, but no site claims that hostname"
	case "no_matching_route":
		return "the site has no route for that path and no catch-all"
	case "unresolvable_upstream":
		return "the destination cannot be determined from configuration"
	case "static_content":
		return "the request is served from disk here"
	case "hop_limit":
		return "the hop limit was reached"
	case "loop_detected":
		return "the request loops"
	}
	return code
}

// sudoersGrant is read-only by construction. Every entry either reports state or
// reads a file; nothing here can change anything on the host.
//
// The entries are the exact commands internal/sshx builds, because sudo matches
// on the command line and a grant that does not match is indistinguishable from
// no grant at all. In particular nagipath never runs `sudo sh -c`: that would be
// a root shell wearing a narrow-grant label.
const sudoersGrant = `# /etc/sudoers.d/nagipath — read-only inspection only
Defaults:nagipath !requiretty

# Vendor dumps: the authoritative effective configuration.
Cmnd_Alias NAGIPATH_DUMP = \
  /usr/sbin/nginx -T, /usr/sbin/nginx -T -c *, /usr/sbin/nginx -V, /usr/sbin/nginx -v, \
  /usr/sbin/nginx -t, /usr/sbin/nginx -t -c *, \
  /usr/sbin/httpd -V, /usr/sbin/httpd -t -D *, /usr/sbin/apache2 -V, /usr/sbin/apache2 -t -D *, \
  /usr/sbin/apache2ctl -V, /usr/sbin/apache2ctl -S, \
  /usr/sbin/haproxy -vv, /usr/sbin/haproxy -c -f *

# Reads. head/stat/tail rather than cat, because those are what nagipath runs:
# it reads a bounded prefix of a file, never an unbounded one.
Cmnd_Alias NAGIPATH_READ = \
  /usr/bin/head -c * -- /etc/nginx/*, /usr/bin/head -c * -- /etc/httpd/*, \
  /usr/bin/head -c * -- /etc/apache2/*, /usr/bin/head -c * -- /etc/haproxy/*, \
  /usr/bin/stat -c * -- /etc/nginx/*, /usr/bin/stat -c * -- /etc/httpd/*, \
  /usr/bin/stat -c * -- /etc/apache2/*, /usr/bin/stat -c * -- /etc/haproxy/*, \
  /usr/bin/find /etc/nginx *, /usr/bin/find /etc/httpd *, \
  /usr/bin/find /etc/apache2 *, /usr/bin/find /etc/haproxy *, \
  /usr/bin/openssl x509 -noout *

# Access logs, tail only, and only under the log directories. This is what a Probe
# reads to prove which instance handled a request; without it a hop can never get
# past Inferred, which is why it is part of this grant and not optional.
Cmnd_Alias NAGIPATH_LOGS = \
  /usr/bin/tail -c * -- /var/log/nginx/*, /usr/bin/tail -c * -- /var/log/httpd/*, \
  /usr/bin/tail -c * -- /var/log/apache2/*, /usr/bin/tail -c * -- /var/log/haproxy/*, \
  /usr/bin/tail -c * -- /var/log/haproxy.log

nagipath ALL=(root) NOPASSWD: NAGIPATH_DUMP, NAGIPATH_READ, NAGIPATH_LOGS`
