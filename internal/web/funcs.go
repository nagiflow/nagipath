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
	"bytes":       humanBytes,
	"join":        join,
	"reason":      reasonText,
	"sudoers":     func() string { return sudoersGrant },
	"expiry":      expiry,
	"expclass":    expclass,
	"ts":          tsHTML,
	"when":        when,
	"date":        dateOnly,
	"clock":       clock,
	"uncovered":   uncovered,
	"on":          navOn,
	"windows":     certWindows,
	"certWindows": certWindows,
	"has":         has,
	"csv":         csv,
	"bar":         bar,
	"hl":          highlight,
	"routing":     routingRules,
	"levels":      hopLevels,
	"hoplabels":   hopLabels,
	"probed":      probed,
	"globals":     globalRules,
	"fired":       fired,
	"firedcount":  firedCount,
	"scopecount":  scopeCount,
	"collapsed":   collapsedCount,
	"nexturl":     nextURL,
	"reasons":     endingReasons,
	"sitekind":    siteKind,
	"matched":     matchedBy,
	"routekind":   routeKind,
	"ports":       sitePorts,
	"upcount":     siteUpstreams,
	"target":      routeTarget,
	"prov1":       firstProv,
	"toolbar":     toolbar,
	"stat":        stat,
	"statif":      statif,
	"stats":       stats,
	"note":        note,
	"warnif":      warnif,
	"cols":        cols,
	"settingsNav": settingsNav,
	"outcome":     collectionOutcome,
	// The same predicate /nodes, the node page and the dashboard all decide the
	// quarantine badge with. Inlined as `ge .Failures .Threshold` it read a missing
	// threshold as zero and stamped QUARANTINED on a fleet that was fine.
	"quarantined": store.Quarantined,
	// What a directive does, in a sentence, for the operator who has never seen
	// it. Unknown directives come back verbatim rather than guessed at.
	"rulesummary": store.RuleSummaryText,
	// The three halves of a badge: its tone, its word, and why it says that.
	"bdg":        badgeClass,
	"ic":         icon,
	"label":      badgeLabel,
	"why":        badgeWhy,
	"num":        num,
	"shortid":    shortid,
	"nodecount":  nodeCount,
	"plural":     plural,
	"add":        func(a, b int) int { return a + b },
	"sub":        func(a, b int) int { return a - b },
	"mod":        func(a, b int) int { return a % b },
	"notglobal":  notGlobal,
	"globalonly": globalOnly,
	"split":      strings.Split,
	"tail":       tailPath,
	"procname":   procName,
}

// procName is the process label shown next to a node's hostname and in the
// process picker. Vendor alone is what an operator thinks of ("this box runs
// nginx"); the config file name only earns a place when the node runs more
// than one instance of the same vendor and the file is what tells them apart.
func procName(instances []store.Instance, vendor, displayName string) string {
	n := 0
	for _, in := range instances {
		if in.Vendor == vendor {
			n++
		}
	}
	if n > 1 {
		return displayName
	}
	return vendor
}

// tailPath shortens a config path to its last two segments. Provenance columns are
// narrow and every path in one instance shares a long prefix, so the cell rendered
// "/usr/local/etc/haprox…" on every row — the identifying half, the file name, is
// the half that got cut. The full path stays in the link's title.
//
// Two segments rather than one because a bare base name is ambiguous exactly where
// it matters: nginx keeps a "default" in both sites-enabled and conf.d — and that
// ambiguity only arises for short names, which is why the length rule below can
// drop the directory without reintroducing it.
func tailPath(p string) string {
	cut := strings.LastIndexByte(p, '/')
	if cut <= 0 {
		return p
	}
	i := strings.LastIndexByte(p[:cut], '/')
	if i <= 0 {
		return p
	}
	// The file name wins when both do not fit. "…/sites-enabled/20-shop.example.com.conf"
	// is wider than the column, and the browser cuts the end — so keeping the
	// directory cost the operator the one segment that identifies the file.
	if len(p)-i > 30 {
		return "…" + p[cut:]
	}
	return "…" + p[i:]
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

// sitePorts is the listener ports a Site answers on ("443 ssl · 80"). A Site
// holds listener ids and the Listener rows hang off the Inst, so the join lives
// here rather than as three nested loops and an `index` in the template.
func sitePorts(inst *trace.Inst, s *trace.Site) string {
	if inst == nil || s == nil {
		return ""
	}
	var out []string
	for _, id := range s.ListenerIDs {
		l := inst.Listeners[id]
		if l == nil {
			continue
		}
		p := strconv.Itoa(l.Port)
		if l.TLS {
			p += " ssl"
		}
		out = append(out, p)
	}
	return strings.Join(out, " · ")
}

// siteUpstreams is how many distinct pools a site's routes proxy to. Distinct,
// because ten locations pointing at one pool is one upstream to keep alive, not
// ten. Routes that resolve to no pool (a file root, a redirect) count for nothing.
func siteUpstreams(s *trace.Site) int {
	if s == nil {
		return 0
	}
	seen := map[int64]bool{}
	for _, r := range s.Routes {
		if r.UpstreamID.Valid {
			seen[r.UpstreamID.Int64] = true
		}
	}
	return len(seen)
}

// routeTarget is where a Route sends the request, in one phrase: the upstream
// pool it proxies to, or whatever the vendor wrote if it resolves to no pool.
// An unresolvable target says so — "—" would read as "nothing configured",
// which is a different fact from "configured, and we cannot follow it".
func routeTarget(inst *trace.Inst, r *trace.Route) string {
	if r == nil {
		return ""
	}
	if r.UpstreamID.Valid && inst != nil {
		if up := inst.Upstreams[r.UpstreamID.Int64]; up != nil {
			return "proxy → " + up.Name
		}
	}
	if r.TargetRaw != "" {
		return r.TargetRaw
	}
	return "not determinable from configuration"
}

// firstProv is the provenance of the first rule that has any, so a row can carry
// one file:byte link without the template looping and breaking out of the loop.
// nil when nothing in the group recorded a file — the caller renders no link
// rather than a link to nowhere.
func firstProv(rules []*trace.Rule) *trace.Rule {
	for _, r := range rules {
		if r != nil && r.FileID != 0 {
			return r
		}
	}
	return nil
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

// fired is what the Trace page's "Rules fired" table lists: the rules that
// decided this request, in the order they were walked, with global-scope tuning
// directives left out. Same reasoning as notGlobal on Rule lookup (f56ba21), and
// it matters more here: on the lab's haproxy, hop 0 listed 80 "rules fired" and
// 75 of them were log format, thread count, stats socket and cipher lists. The
// panel footer states the count it left out, so nothing is silently dropped.
func fired(rules []trace.HopRule) []trace.HopRule {
	var out []trace.HopRule
	for _, hr := range rules {
		if hr.Scope != "global" {
			out = append(out, hr)
		}
	}
	return out
}

// firedOf is the summary line's "11 of 31 rules fired": the routing rules that
// decided the request, out of every rule that was in scope at these hops. The
// difference is global directives, which apply to every request identically, and
// shadowed rules, which an inner directive discarded — neither is a decision
// about this request, and counting them as ones fired would overstate the answer.
func firedOf(hops []*trace.Hop) (int, int) {
	var f, total int
	for _, h := range hops {
		f += len(fired(h.Rules))
		total += len(h.Rules) + len(h.Shadowed)
	}
	return f, total
}

func firedCount(hops []*trace.Hop) int { f, _ := firedOf(hops); return f }
func scopeCount(hops []*trace.Hop) int { _, n := firedOf(hops); return n }

// collapsedCount is the rules the table does not show, which its footer names:
// global tuning directives plus rules an inner scope shadowed.
func collapsedCount(hops []*trace.Hop) int {
	f, total := firedOf(hops)
	return total - f
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
// The windows are cumulative, because that is what the labels say: a certificate
// with six days left is also one with under thirty. Exclusive buckets printed
// "≤ 7 days 1" beside "≤ 30 days 1" on a fleet holding two expiring certificates,
// so the tile an operator escalates on undercounted the problem.
//
// The sub-line is the binding count, because "9" is nine certificates and the
// number that matters when one expires is how many sites stop working.
func certWindows(list []store.CertificateView) []Stat {
	var expired, d7, d30 int
	var expiredB, d7B, d30B, bindings int
	for _, c := range list {
		bindings += c.Bindings
		t, err := time.Parse(time.RFC3339, c.NotAfter)
		if err != nil {
			continue
		}
		days := int(time.Until(t).Hours() / 24)
		switch {
		case days < 0:
			expired, expiredB = expired+1, expiredB+c.Bindings
		case days <= 7:
			d7, d7B = d7+1, d7B+c.Bindings
			d30, d30B = d30+1, d30B+c.Bindings
		case days <= 30:
			d30, d30B = d30+1, d30B+c.Bindings
		}
	}
	return []Stat{
		// Worst first: what has already stopped working outranks what will.
		{N: expired, Label: "expired", Tone: warnif(expired), Note: plural(expiredB, "binding")},
		{N: d7, Label: "≤ 7 days", Tone: warnif(d7), Note: plural(d7B, "binding")},
		{N: d30, Label: "≤ 30 days", Tone: warnif(d30), Note: plural(d30B, "binding")},
		{N: len(list), Label: "total distinct", Note: "by fingerprint"},
	}
}

// plural is "1 binding" / "3 bindings", which is the whole of what the sub-lines
// on a stat row need.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return num(n) + " " + word + "s"
}

// shortid is the first six characters of a correlation token, which is what
// identifies a probe on screen without being 32 characters of the row's width.
// Not `slice .Token 0 6` in the template: that is a render error on any token
// shorter than six, and the whole page dies with it.
func shortid(s string) string {
	if len(s) <= 6 {
		return s
	}
	return s[:6]
}

// nodeCount turns the store's comma-joined list of node display names into the
// count the certificates table wants under its bindings number. The template
// used to print the list itself followed by the word "hosts", which read
// "app01 hosts".
func nodeCount(hosts string) string {
	if hosts == "" {
		return "unbound"
	}
	return plural(strings.Count(hosts, ",")+1, "node")
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

// expclass grades a NotAfter into a badge tone, using the same 30-day window the
// stats row above the list counts with so the badge and the figure agree. It
// exists because `lt (expiry .NotAfter) 0` compares a sentence to a number — a
// template error raised at render time, which is how it reached a browser.
func expclass(notAfter string) string {
	t, err := time.Parse(time.RFC3339, notAfter)
	if err != nil {
		return "inf"
	}
	switch days := int(time.Until(t).Hours() / 24); {
	case days < 0:
		return "err"
	case days <= 30:
		return "deg"
	default:
		// A certificate with months left is a fact, not a warning.
		return "none"
	}
}

// when is a timestamp in the words that answer "is this current?" without making
// the reader do arithmetic. Under a week it is relative, older than that it is a
// date: "6 days ago" is useful and "63 days ago" is not.
func when(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Not a timestamp we recognise. Printing it unchanged is honest; printing
		// nothing would hide a stored value from the operator.
		return ts
	}
	d := time.Since(t)
	switch {
	// A future timestamp is a clock difference between here and the node, not an
	// event that has not happened, so it is not phrased as one.
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.UTC().Format("2 Jan 2006 15:04Z")
	}
}

// dateOnly is for the timestamps `when` would misread: a certificate's validity
// runs into the future, where "ago" is nonsense, and the day is what an operator
// puts in a calendar. To the second is a machine's answer to "when does this
// expire?"; the distance in words is what the expiry badge beside it is for.
func dateOnly(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format("2 Jan 2006")
}

// clock is the wall-clock form the wireframe uses wherever a cell says when a
// collection last ran — "04:12Z", not "3h ago". The two are not interchangeable:
// a relative distance answers "is this fresh?", which a freshness badge beside it
// already answers, while an operator comparing four clusters in one table needs
// the times to line up, and "3h ago" on rows rendered a minute apart does not.
//
// Always UTC and always stamped Z, because a fleet spans zones and a bare 04:12
// in a ticket is a question rather than a fact. The date appears only when the
// timestamp is not from today, so the common case stays five characters wide.
func clock(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	t = t.UTC()
	if now := time.Now().UTC(); t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04Z")
	}
	return t.Format("2 Jan 15:04Z")
}

// tsHTML is how every screen prints a stored time: the short form in the text and
// the exact value on hover. Twenty-one places were printing raw RFC3339, which is
// a timestamp only a machine reads at a glance.
func tsHTML(ts string) template.HTML {
	if ts == "" {
		return ""
	}
	return template.HTML(`<span title="` + template.HTMLEscapeString(ts) + `">` +
		template.HTMLEscapeString(when(ts)) + `</span>`)
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
