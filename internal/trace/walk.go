package trace

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const hopLimit = 10

// Terminal reasons. The set is closed and has no `unknown`: a Trace that ends
// without an explanation is the failure mode this enum exists to prevent.
const (
	TermExternal     = "external_hop"
	TermNoListener   = "no_matching_listener"
	TermNoSite       = "no_matching_site"
	TermNoRoute      = "no_matching_route"
	TermUnresolvable = "unresolvable_upstream"
	TermStatic       = "static_content"
	TermHopLimit     = "hop_limit"
	TermLoop         = "loop_detected"
)

type Query struct {
	Scheme   string
	Hostname string
	Path     string
	Port     int
}

// Normalise fills the port in from the scheme, which is the one default an
// operator should never have to type.
func (q Query) Normalise() Query {
	if q.Scheme == "" {
		q.Scheme = "https"
	}
	if q.Path == "" {
		q.Path = "/"
	}
	if q.Port == 0 {
		if q.Scheme == "https" {
			q.Port = 443
		} else {
			q.Port = 80
		}
	}
	q.Hostname = strings.ToLower(q.Hostname)
	return q
}

type Candidate struct {
	Inst      *Inst
	Listener  *Listener
	Site      *Site
	MatchedBy string
	Selected  bool
	Reason    string
}

type HopRule struct {
	Rule       *Rule
	Inherited  bool
	Scope      string
	Confidence string
}

type Change struct {
	Rule *Rule
	From string
	To   string
}

type Next struct {
	Member            *Member
	ResolvedAddresses []string
	InstanceID        int64
	HopOrdinal        int
}

type Branch struct {
	HopOrdinal int
	RouteID    int64
	Raw        string
	Reason     string
}

type Hop struct {
	Ordinal    int
	IsExternal bool
	Inst       *Inst
	Listener   *Listener
	Site       *Site
	MatchedBy  string
	Route      *Route
	Precedence string
	Upstream   *Upstream

	InboundPath   string
	EffectivePath string
	Confidence    string

	Rules         []HopRule
	PathChangedBy []Change
	Shadowed      []HopRule
	Next          []Next

	// Incomplete is the reason this Hop's Snapshot could not be read in full, if
	// it could not. Set whether or not the Hop is on the followed path.
	Incomplete string

	ExternalTarget string
	ExternalReason string
	ArrivedFrom    int // -1 for the entry hop

	// Terminal is set on a Hop that ends its branch.
	Terminal string
}

type Trace struct {
	ID              int64
	Query           Query
	ComputedAt      string
	ParserVersion   int
	SnapshotSet     []int64
	Confidence      string
	TerminalReason  string
	Hops            []*Hop
	EntryCandidates []Candidate
	Undetermined    []Branch
	Notes           []string
}

// pending is one queued step: a request arriving at an Instance.
type pending struct {
	inst      *Inst
	hostname  string
	path      string
	port      int
	tls       bool
	from      int
	memberIdx int // index into the parent hop's Next, so it can be back-linked
}

// Walk computes the Trace. It is pure: everything it needs is in the Topology, so
// the same inputs always produce the same Trace.
func Walk(top *Topology, q Query) *Trace {
	q = q.Normalise()
	tr := &Trace{Query: q, Confidence: "inferred", ParserVersion: 1}

	tr.EntryCandidates = entryCandidates(top, q)
	if len(tr.EntryCandidates) == 0 {
		tr.TerminalReason = terminalForNoEntry(top, q)
		tr.Notes = append(tr.Notes, noEntryNote(top, q))
		return tr
	}

	var entry *Candidate
	for i := range tr.EntryCandidates {
		if tr.EntryCandidates[i].Selected {
			entry = &tr.EntryCandidates[i]
			break
		}
	}

	seen := map[string]bool{}
	snapshots := map[int64]bool{}
	queue := []pending{{
		inst: entry.Inst, hostname: q.Hostname, path: q.Path,
		port: entry.Listener.Port, tls: entry.Listener.TLS, from: -1, memberIdx: -1,
	}}

	for len(queue) > 0 {
		step := queue[0]
		queue = queue[1:]

		if len(tr.Hops) >= hopLimit {
			tr.Hops = append(tr.Hops, &Hop{
				Ordinal: len(tr.Hops), ArrivedFrom: step.from, IsExternal: true,
				Confidence: "inferred", InboundPath: step.path, EffectivePath: step.path,
				ExternalTarget: step.hostname,
				ExternalReason: fmt.Sprintf("the %d-Hop limit was reached; the truncation is itself the finding", hopLimit),
				Terminal:       TermHopLimit,
			})
			tr.record(step, TermHopLimit)
			break
		}

		hop := &Hop{
			Ordinal: len(tr.Hops), Inst: step.inst, ArrivedFrom: step.from,
			InboundPath: step.path, EffectivePath: step.path, Confidence: "inferred",
		}
		tr.Hops = append(tr.Hops, hop)
		snapshots[step.inst.SnapshotID] = true
		tr.link(step, hop.Ordinal)

		// An unparsed Snapshot stops the walk here. An empty answer that looks
		// complete is the worst possible output (trace.md §7).
		if step.inst.ParseState != "parsed" {
			hop.Terminal = TermUnresolvable
			hop.ExternalReason = "the current Snapshot for " + step.inst.DisplayName +
				" is not parsed, so nagipath will not guess what it routes"
			hop.Confidence = "partial"
			tr.degrade(hop.Ordinal)
			tr.record(step, TermUnresolvable)
			continue
		}
		if step.inst.Degraded {
			hop.Confidence = "partial"
			hop.Incomplete = step.inst.DegradedReason
			// The banner is for the path the trace followed. A branch's incomplete
			// snapshot is a caveat about that branch and is reported on its own Hop:
			// four lines of nginx include errors at the top of the answer buries the
			// answer, and the operator cannot tell which hop they are about.
			if tr.degrade(hop.Ordinal) {
				tr.Notes = append(tr.Notes, step.inst.NodeName+"/"+step.inst.Vendor+
					": configuration is incomplete ("+step.inst.DegradedReason+")")
			}
		}

		l, site, matchedBy := selectSite(step.inst, step.hostname, step.port, step.tls)
		if l == nil {
			hop.Terminal = TermNoListener
			hop.ExternalReason = fmt.Sprintf("%s has no listener on port %d with %s",
				step.inst.DisplayName, step.port, tlsWord(step.tls))
			tr.record(step, TermNoListener)
			continue
		}
		hop.Listener = l
		if site == nil {
			hop.Terminal = TermNoSite
			hop.ExternalReason = fmt.Sprintf("no site on %s claims the hostname %s",
				step.inst.DisplayName, step.hostname)
			tr.record(step, TermNoSite)
			continue
		}
		hop.Site, hop.MatchedBy = site, matchedBy

		route, explanation, branches := selectRoute(step.inst.Vendor, site, step.path)
		for _, b := range branches {
			b.HopOrdinal = hop.Ordinal
			tr.Undetermined = append(tr.Undetermined, b)
		}
		if route == nil {
			hop.Terminal = TermNoRoute
			hop.ExternalReason = fmt.Sprintf("no route on site %s matches %s, and there is no catch-all",
				site.PrimaryName, step.path)
			tr.record(step, TermNoRoute)
			continue
		}
		hop.Route, hop.Precedence = route, explanation

		key := fmt.Sprintf("%d:%d", step.inst.ID, route.ID)
		if seen[key] {
			hop.Terminal = TermLoop
			hop.ExternalReason = "this route was already taken in this trace; a request loop is a real misconfiguration, not a tool error"
			tr.record(step, TermLoop)
			continue
		}
		seen[key] = true

		hop.Rules, hop.Shadowed = effectiveRules(step.inst, site, route)
		var rewriteBranches []Branch
		hop.EffectivePath, hop.PathChangedBy, rewriteBranches = rewritePath(step.inst.Vendor, step.path, route, hop.Rules)
		for _, b := range rewriteBranches {
			b.HopOrdinal = hop.Ordinal
			tr.Undetermined = append(tr.Undetermined, b)
		}

		if !route.UpstreamID.Valid {
			if isStatic(route, hop.Rules, site) {
				hop.Terminal = TermStatic
				hop.ExternalReason = "this route serves files from disk and does not proxy"
				tr.record(step, TermStatic)
				continue
			}
			hop.Terminal = TermUnresolvable
			hop.ExternalReason = unresolvableReason(route)
			tr.record(step, TermUnresolvable)
			continue
		}

		up := step.inst.Upstreams[route.UpstreamID.Int64]
		if up == nil || len(up.Members) == 0 {
			hop.Terminal = TermUnresolvable
			hop.ExternalReason = "the route targets an upstream with no members"
			tr.record(step, TermUnresolvable)
			continue
		}
		hop.Upstream = up

		for _, m := range up.Members {
			// Resolved on step.inst's own Node, never any other — the Upstream Member
			// line being resolved lives in step.inst's own configuration, so that
			// Node's getent hosts answer is the only one that was ever a valid answer
			// to "what does this route to" (ADR-0010).
			addrs := top.Resolve(step.inst.NodeID, m.Host)
			n := Next{Member: m, ResolvedAddresses: addrs, HopOrdinal: -1}
			targetPort := m.Port
			if targetPort == 0 {
				targetPort = defaultPort(m.Scheme)
			}

			target := pickInstance(top, step.inst.NodeID, m.Host, targetPort)
			if target != nil {
				n.InstanceID = target.ID
				hop.Next = append(hop.Next, n)
				queue = append(queue, pending{
					inst: target, hostname: step.hostname, path: hop.EffectivePath,
					port: targetPort, tls: m.Scheme == "https",
					from: hop.Ordinal, memberIdx: len(hop.Next) - 1,
				})
				continue
			}
			hop.Next = append(hop.Next, n)
			// An external destination is the most common and most useful ending. The
			// reason names what we found so the operator can act on it.
			ext := &Hop{
				Ordinal: len(tr.Hops), IsExternal: true, ArrivedFrom: hop.Ordinal,
				Confidence: "inferred", InboundPath: hop.EffectivePath,
				EffectivePath:  hop.EffectivePath,
				ExternalTarget: net.JoinHostPort(m.Host, strconv.Itoa(targetPort)),
				ExternalReason: externalReason(top, step.inst.NodeID, m.Host, targetPort, addrs),
				Terminal:       TermExternal,
			}
			tr.Hops = append(tr.Hops, ext)
			hop.Next[len(hop.Next)-1].HopOrdinal = ext.Ordinal
			if tr.TerminalReason == "" && step.from == -1 {
				tr.TerminalReason = TermExternal
			}
		}
	}

	if tr.TerminalReason == "" {
		// The primary chain never terminated explicitly, which happens when every
		// branch it produced led to another managed Instance that then ended. Take
		// the last terminal reason on the chain descending from the entry hop.
		tr.TerminalReason = lastTerminal(tr.Hops)
	}
	for id := range snapshots {
		tr.SnapshotSet = append(tr.SnapshotSet, id)
	}
	sort.Slice(tr.SnapshotSet, func(i, j int) bool { return tr.SnapshotSet[i] < tr.SnapshotSet[j] })
	return tr
}

// record sets the Trace-level terminal reason from the primary chain only: the
// chain that starts at the entry Hop. Branch endings are reported per Hop.
func (tr *Trace) record(step pending, reason string) {
	if tr.TerminalReason == "" && (step.from == -1 || tr.onPrimary(step.from)) {
		tr.TerminalReason = reason
	}
}

// degrade lowers the Trace's own confidence to partial when the Hop that is
// incomplete is on the path the trace followed, and reports whether it did. A
// second upstream member with a bad Snapshot is a branch: it is worth a note on
// that Hop, but it does not make the answer about the followed path any less
// certain, and marking every Trace partial because one node in the fleet is
// unreadable makes the badge mean nothing.
func (tr *Trace) degrade(ordinal int) bool {
	if !tr.onPrimary(ordinal) {
		return false
	}
	tr.Confidence = "partial"
	return true
}

func (tr *Trace) onPrimary(ordinal int) bool {
	for ordinal >= 0 && ordinal < len(tr.Hops) {
		h := tr.Hops[ordinal]
		if h.ArrivedFrom == -1 {
			return true
		}
		// Only the first branch out of a Hop continues the primary chain.
		parent := tr.Hops[h.ArrivedFrom]
		if len(parent.Next) == 0 || parent.Next[0].HopOrdinal != ordinal {
			return false
		}
		ordinal = h.ArrivedFrom
	}
	return false
}

func (tr *Trace) link(step pending, ordinal int) {
	if step.from < 0 || step.memberIdx < 0 || step.from >= len(tr.Hops) {
		return
	}
	parent := tr.Hops[step.from]
	if step.memberIdx < len(parent.Next) {
		parent.Next[step.memberIdx].HopOrdinal = ordinal
	}
}

func lastTerminal(hops []*Hop) string {
	for i := len(hops) - 1; i >= 0; i-- {
		if hops[i].Terminal != "" {
			return hops[i].Terminal
		}
	}
	return TermExternal
}

// ------------------------------------------------------------ entry selection

// entryCandidates finds every Instance that could receive this request. This is
// fleet-wide on purpose: in real fleets the same hostname is often served by an
// edge proxy *and* by the server behind it, and picking one arbitrarily misleads
// the operator (trace.md §3.1).
func entryCandidates(top *Topology, q Query) []Candidate {
	var out []Candidate
	for _, in := range top.Instances {
		l, site, matchedBy := selectSite(in, q.Hostname, q.Port, q.Scheme == "https")
		if l == nil || site == nil {
			continue
		}
		out = append(out, Candidate{Inst: in, Listener: l, Site: site, MatchedBy: matchedBy})
	}
	if len(out) == 0 {
		return nil
	}

	// A candidate that another candidate proxies to is not the entry: the one
	// fronting it is. Anything nothing else points at is an entry.
	frontedBy := map[int64]string{}
	for _, c := range out {
		for _, other := range out {
			if other.Inst.ID == c.Inst.ID {
				continue
			}
			if proxiesTo(top, other.Inst, c.Inst) {
				frontedBy[c.Inst.ID] = other.Inst.NodeName + "/" + other.Inst.Vendor
			}
		}
	}
	chosen := false
	for i := range out {
		if front, fronted := frontedBy[out[i].Inst.ID]; fronted {
			out[i].Reason = "also answers this hostname; not selected as entry because " +
				front + " fronts it in this trace"
			continue
		}
		if chosen {
			out[i].Reason = "also answers this hostname on this port; nothing in the fleet fronts it either"
			continue
		}
		out[i].Selected, chosen = true, true
		out[i].Reason = fmt.Sprintf("%s matched by %s", out[i].Site.PrimaryName, out[i].MatchedBy)
	}
	if !chosen {
		// Every candidate fronts another: a cycle. Pick the first and say so.
		out[0].Selected = true
		out[0].Reason = "every candidate is fronted by another; entering at the first one"
	}
	return out
}

func proxiesTo(top *Topology, from, to *Inst) bool {
	for _, up := range from.Upstreams {
		for _, m := range up.Members {
			port := m.Port
			if port == 0 {
				port = defaultPort(m.Scheme)
			}
			for _, cand := range top.InstancesAt(from.NodeID, m.Host) {
				if cand.ID == to.ID && listensOn(to, port) {
					return true
				}
			}
		}
	}
	return false
}

func pickInstance(top *Topology, nodeID int64, host string, port int) *Inst {
	for _, in := range top.InstancesAt(nodeID, host) {
		if listensOn(in, port) {
			return in
		}
	}
	return nil
}

func listensOn(in *Inst, port int) bool {
	for _, l := range in.Listeners {
		if l.Port == port {
			return true
		}
	}
	return false
}

func externalReason(top *Topology, nodeID int64, host string, port int, addrs []string) string {
	if len(addrs) == 0 && net.ParseIP(host) == nil {
		return host + " could not be resolved on the node that holds this upstream"
	}
	for _, in := range top.InstancesAt(nodeID, host) {
		return fmt.Sprintf("%s is a managed node, but no instance on it listens on port %d",
			in.NodeName, port)
	}
	return fmt.Sprintf("%s resolves to an address that is not a managed node", host)
}

func terminalForNoEntry(top *Topology, q Query) string {
	// Distinguish "nothing listens there" from "something listens but no site
	// claims the hostname" — the two send an operator to different places.
	for _, in := range top.Instances {
		for _, l := range in.Listeners {
			if l.Port == q.Port && l.TLS == (q.Scheme == "https") {
				return TermNoSite
			}
		}
	}
	return TermNoListener
}

func noEntryNote(top *Topology, q Query) string {
	var ports []string
	seen := map[int]bool{}
	for _, in := range top.Instances {
		for _, l := range in.Listeners {
			if !seen[l.Port] {
				seen[l.Port] = true
				ports = append(ports, strconv.Itoa(l.Port))
			}
		}
	}
	sort.Strings(ports)
	if len(ports) == 0 {
		return "no instance in the fleet has any listener yet; collect a node first"
	}
	return "ports currently listening across the fleet: " + strings.Join(ports, ", ")
}

func tlsWord(tls bool) string {
	if tls {
		return "TLS"
	}
	return "plaintext"
}

func defaultPort(scheme string) int {
	if scheme == "https" {
		return 443
	}
	return 80
}

// -------------------------------------------------------------- site matching

// selectSite picks the Listener and Site that would receive the request, in the
// vendor-independent order: exact name, wildcard prefix, wildcard suffix, regex,
// then catch-all.
func selectSite(in *Inst, hostname string, port int, tls bool) (*Listener, *Site, string) {
	var listeners []*Listener
	for _, l := range in.Listeners {
		if l.Port != port {
			continue
		}
		// A listener declared without ssl on a TLS request is not a match: guessing
		// otherwise is how a plaintext listener gets reported as encrypted.
		if l.TLS != tls {
			continue
		}
		listeners = append(listeners, l)
	}
	sort.Slice(listeners, func(i, j int) bool { return listeners[i].ID < listeners[j].ID })
	if len(listeners) == 0 {
		return nil, nil, ""
	}

	best := 99
	var bestSite *Site
	var bestListener *Listener
	var reason string
	for _, l := range listeners {
		for _, s := range in.Sites {
			if !boundTo(s, l) {
				continue
			}
			rank, why := nameRank(s, hostname)
			if rank < 0 {
				continue
			}
			if rank == 4 && l.IsDefault {
				rank = 3 // default_server outranks a bare catch-all
				why = "default_server catch-all"
			}
			if rank < best {
				best, bestSite, bestListener, reason = rank, s, l, why
			}
		}
	}
	if bestSite == nil {
		return listeners[0], nil, ""
	}
	return bestListener, bestSite, reason
}

func boundTo(s *Site, l *Listener) bool {
	if len(s.ListenerIDs) == 0 {
		return true // a site with no explicit binding answers on any listener
	}
	for _, id := range s.ListenerIDs {
		if id == l.ID {
			return true
		}
	}
	return false
}

// nameRank scores how a Site claims a hostname. Lower is a stronger claim; -1
// means it does not claim it at all.
func nameRank(s *Site, hostname string) (int, string) {
	rank, why := -1, ""
	better := func(r int, w string) {
		if rank == -1 || r < rank {
			rank, why = r, w
		}
	}
	for _, n := range s.Names {
		name := strings.ToLower(n.Name)
		switch n.MatchKind {
		case "exact":
			if name == hostname {
				better(0, "exact name "+n.Name)
			}
		case "wildcard_prefix": // *.example.com
			if suffix, ok := strings.CutPrefix(name, "*"); ok &&
				strings.HasSuffix(hostname, suffix) && len(hostname) > len(suffix) {
				better(1, "wildcard "+n.Name)
			}
		case "wildcard_suffix": // www.*
			if prefix, ok := strings.CutSuffix(name, "*"); ok && strings.HasPrefix(hostname, prefix) {
				better(2, "wildcard "+n.Name)
			}
		case "regex":
			if re, err := regexp.Compile(strings.TrimPrefix(strings.TrimPrefix(name, "~*"), "~")); err == nil &&
				re.MatchString(hostname) {
				better(3, "regex "+n.Name)
			}
		case "catch_all":
			better(4, "catch-all (no site names this hostname)")
		}
	}
	return rank, why
}

// ------------------------------------------------------------- route matching

// selectRoute applies the vendor's own matching model. The two models are
// genuinely different and conflating them is the single most likely way to be
// confidently wrong:
//
//   - NGINX evaluates in rank order and the FIRST match wins.
//   - Apache MERGES sections in rank order, so the LAST applicable one wins,
//     which is why <Location> beats <Directory>.
func selectRoute(vendor string, s *Site, path string) (*Route, string, []Branch) {
	routes := flatten(s.Routes)
	switch vendor {
	case "nginx":
		return nginxRoute(routes, path)
	case "apache":
		return apacheRoute(routes, path)
	default:
		return haproxyRoute(routes, path)
	}
}

func flatten(routes []*Route) []*Route {
	var out []*Route
	for _, r := range routes {
		out = append(out, r)
		out = append(out, flatten(r.Children)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func nginxRoute(routes []*Route, path string) (*Route, string, []Branch) {
	var branches []Branch
	for _, r := range routes {
		if r.MatchType == "exact" && r.Pattern == path {
			return r, "location = " + r.Pattern + " is an exact match, which nginx checks first and never overrides", nil
		}
	}

	var bestPrefix *Route
	for _, r := range routes {
		if r.MatchType != "prefix" && r.MatchType != "prefix_no_regex" {
			continue
		}
		if !strings.HasPrefix(path, r.Pattern) {
			continue
		}
		if bestPrefix == nil || len(r.Pattern) > len(bestPrefix.Pattern) {
			bestPrefix = r
		}
	}
	if bestPrefix != nil && bestPrefix.MatchType == "prefix_no_regex" {
		return bestPrefix, "location ^~ " + bestPrefix.Pattern +
			" is the longest matching prefix and ^~ stops nginx from checking regex locations at all", nil
	}

	for _, r := range routes {
		if r.MatchType != "regex" && r.MatchType != "regex_ci" {
			continue
		}
		re, err := compileNginxRegex(r)
		if err != nil {
			branches = append(branches, Branch{RouteID: r.ID, Raw: r.Pattern,
				Reason: "regex location could not be evaluated: " + err.Error()})
			continue
		}
		if re.MatchString(path) {
			return r, "location " + prefixFor(r) + " " + r.Pattern +
				" is the first regex location in file order that matches; regex order is file order, not length", branches
		}
	}

	if bestPrefix != nil {
		return bestPrefix, "location " + bestPrefix.Pattern +
			" is the longest matching prefix, and no exact or regex location matched first", branches
	}
	return nil, "", branches
}

func compileNginxRegex(r *Route) (*regexp.Regexp, error) {
	expr := r.Pattern
	if r.MatchType == "regex_ci" {
		expr = "(?i)" + expr
	}
	return regexp.Compile(expr)
}

func prefixFor(r *Route) string {
	if r.MatchType == "regex_ci" {
		return "~*"
	}
	return "~"
}

func apacheRoute(routes []*Route, path string) (*Route, string, []Branch) {
	var branches []Branch
	var best *Route
	for _, r := range routes {
		ok, err := apacheMatches(r, path)
		if err != nil {
			branches = append(branches, Branch{RouteID: r.ID, Raw: r.Pattern,
				Reason: "container pattern could not be evaluated: " + err.Error()})
			continue
		}
		if !ok {
			continue
		}
		// Later merge wins, and on a tie the more specific pattern does — except
		// DirectoryMatch/LocationMatch, whose Specificity the parser deliberately
		// zeroes (apache.go), because a regex has no inherent narrower/wider
		// ordering the way a literal path length does; real Apache resolves those
		// ties by config order instead. routes is already in that order (loaded
		// `ORDER BY site_id, id`), so >= — not > — lets a later same-specificity
		// route (which for regex containers means every tied one) keep overriding
		// rather than freezing on whichever was seen first.
		if best == nil || r.PrecedenceRank > best.PrecedenceRank ||
			(r.PrecedenceRank == best.PrecedenceRank && r.Specificity >= best.Specificity) {
			best = r
		}
	}
	if best == nil {
		return nil, "", branches
	}
	return best, fmt.Sprintf("<%s %s> is the last section to merge for this path; "+
		"Apache merges Directory, then Files, then Location, so Location wins",
		apacheTag(best.MatchType), best.Pattern), branches
}

func apacheMatches(r *Route, path string) (bool, error) {
	switch r.MatchType {
	case "location", "directory":
		return strings.HasPrefix(path, r.Pattern) || r.Pattern == "/", nil
	case "location_match", "directory_match":
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return false, err
		}
		return re.MatchString(path), nil
	case "files":
		return strings.HasSuffix(path, r.Pattern), nil
	case "files_match":
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return false, err
		}
		return re.MatchString(path), nil
	}
	return false, nil
}

func apacheTag(matchType string) string {
	switch matchType {
	case "location":
		return "Location"
	case "location_match":
		return "LocationMatch"
	case "directory":
		return "Directory"
	case "directory_match":
		return "DirectoryMatch"
	case "files":
		return "Files"
	case "files_match":
		return "FilesMatch"
	}
	return matchType
}

// haproxyRoute evaluates use_backend rules in file order, then default_backend.
// An ACL the engine cannot evaluate is reported verbatim as an undetermined
// branch rather than assumed false.
func haproxyRoute(routes []*Route, path string) (*Route, string, []Branch) {
	var branches []Branch
	var deflt *Route
	for _, r := range routes {
		if r.MatchType == "haproxy_default_backend" {
			if deflt == nil {
				deflt = r
			}
			continue
		}
		if r.MatchType != "haproxy_acl_use_backend" {
			continue
		}
		if r.Pattern == "" {
			return r, "use_backend with no condition; the first unconditional use_backend wins", branches
		}
		branches = append(branches, Branch{RouteID: r.ID, Raw: "use_backend " + r.TargetRaw + " if " + r.Pattern,
			Reason: "ACL expression is not evaluable from configuration alone"})
	}
	if deflt != nil {
		return deflt, "default_backend " + deflt.TargetRaw +
			", because no use_backend condition could be evaluated as true", branches
	}
	return nil, "", branches
}

// ------------------------------------------------------------ rules and paths

// effectiveRules collects the Rules that apply at this Hop, nearest scope last,
// and works out which inherited ones are discarded.
//
// The NGINX `add_header` rule is the case that matters: a nearer scope declaring
// any add_header discards **every** inherited add_header. That silently drops
// security headers and is genuinely hard to find by reading configuration, which
// is why it is recomputed per request rather than trusted from the inventory.
func effectiveRules(in *Inst, s *Site, r *Route) (applied []HopRule, shadowed []HopRule) {
	var chain []HopRule
	for _, rule := range in.GlobalRules {
		chain = append(chain, HopRule{Rule: rule, Inherited: true, Scope: "global", Confidence: "candidate"})
	}
	for _, rule := range s.Rules {
		chain = append(chain, HopRule{Rule: rule, Inherited: true, Scope: "site", Confidence: "candidate"})
	}
	for _, rule := range r.Rules {
		chain = append(chain, HopRule{Rule: rule, Scope: "route", Confidence: "candidate"})
	}

	if in.Vendor == "nginx" {
		nearest := ""
		for _, hr := range chain {
			if strings.EqualFold(hr.Rule.Directive, "add_header") && !hr.Inherited {
				nearest = "location " + r.Pattern
			}
		}
		if nearest != "" {
			var kept []HopRule
			for _, hr := range chain {
				if strings.EqualFold(hr.Rule.Directive, "add_header") && hr.Inherited {
					hr.Rule = shadowedCopy(hr.Rule, nearest+" declares its own add_header, discarding all inherited add_header directives")
					shadowed = append(shadowed, hr)
					continue
				}
				kept = append(kept, hr)
			}
			return kept, shadowed
		}
	}
	return chain, nil
}

func shadowedCopy(r *Rule, why string) *Rule {
	c := *r
	c.Shadowed = true
	c.ShadowedBy = why
	return &c
}

// rewritePath computes the path after this Hop's rewrites. This is the field the
// core demo turns on: "/api/v2/charge arrives at the edge, /v2/charge arrives at
// the app server", with the Rule that did it named.
func rewritePath(vendor, path string, r *Route, rules []HopRule) (string, []Change, []Branch) {
	var changes []Change
	var branches []Branch
	current := path

	// Labelled, because `break` inside the switch below would only leave the switch.
	// The `last` and [L] flags stop rule processing, and a rewrite chain that keeps
	// going past them produces a path the server never computes.
rewrites:
	for i, hr := range rules {
		rule := hr.Rule
		switch vendor {
		case "nginx":
			if !strings.EqualFold(rule.Directive, "rewrite") {
				continue
			}
			next, ok := applyNginxRewrite(current, rule.Args)
			if !ok {
				continue
			}
			changes = append(changes, Change{Rule: rule, From: current, To: next})
			current = next
			if hasFlag(rule.Args, "break", "last") {
				break rewrites
			}
		case "apache":
			if !strings.EqualFold(rule.Directive, "RewriteRule") {
				continue
			}
			// A RewriteRule is guarded by the RewriteConds immediately above it, and
			// those test things a snapshot does not contain: the Host header, the
			// method, an environment variable. Applying the rule anyway would report
			// a path change that only happens for some requests as one that happens
			// for this one.
			if conds := precedingConds(rules, i); len(conds) > 0 {
				branches = append(branches, Branch{
					RouteID: routeID(r),
					Raw:     rule.Directive + " " + rule.Args,
					Reason: fmt.Sprintf(
						"guarded by %d RewriteCond, which test the request rather than the configuration: %s",
						len(conds), strings.Join(conds, "; ")),
				})
				continue
			}
			next, ok := applyApacheRewrite(current, rule.Args)
			if !ok {
				continue
			}
			changes = append(changes, Change{Rule: rule, From: current, To: next})
			current = next
			if hasFlag(rule.Args, "[L]", "[L,") {
				break rewrites
			}
		case "haproxy":
			next, ok := applyHAProxyPath(current, rule.Directive, rule.Args)
			if !ok {
				continue
			}
			changes = append(changes, Change{Rule: rule, From: current, To: next})
			current = next
		}
	}

	// The vendor's own proxy path mapping runs after rewrites.
	if next, rule, ok := proxyMapping(vendor, current, r, rules); ok {
		if next != current {
			changes = append(changes, Change{Rule: rule, From: current, To: next})
			current = next
		}
	}
	return current, changes, branches
}

// precedingConds returns the RewriteConds that guard the RewriteRule at index i.
// Apache attaches every RewriteCond immediately above a rule to that rule, and
// ANDs them by default — so one unevaluable condition makes the whole rule a
// branch.
func precedingConds(rules []HopRule, i int) []string {
	var out []string
	for j := i - 1; j >= 0; j-- {
		if !strings.EqualFold(rules[j].Rule.Directive, "RewriteCond") {
			break
		}
		out = append([]string{rules[j].Rule.Args}, out...)
	}
	return out
}

func routeID(r *Route) int64 {
	if r == nil {
		return 0
	}
	return r.ID
}

// proxyMapping implements the trailing-slash rule, which is the single most
// misunderstood behaviour in either vendor:
//
//	nginx:  proxy_pass http://api/   strips the matched location prefix
//	        proxy_pass http://api    passes the URI through unchanged
//	apache: ProxyPass /api http://x/ maps /api onto /
func proxyMapping(vendor, path string, r *Route, rules []HopRule) (string, *Rule, bool) {
	target := r.TargetRaw
	if target == "" {
		return path, nil, false
	}
	var rule *Rule
	for _, hr := range rules {
		switch strings.ToLower(hr.Rule.Directive) {
		case "proxy_pass", "proxypass", "proxypassmatch":
			rule = hr.Rule
		}
	}
	// No proxy directive, no proxy mapping. TargetRaw is also set for `root`,
	// `alias` and `return`, and reading a filesystem path out of `root
	// /usr/share/nginx/html` as the request's new path is wrong twice: nothing
	// rewrote the request, and the answer is a disk path, not a URL.
	if rule == nil {
		return path, nil, false
	}

	uriPath := targetPath(target)
	switch vendor {
	case "nginx":
		if uriPath == "" {
			return path, rule, false // no URI component: pass through unchanged
		}
		if r.MatchType == "prefix" || r.MatchType == "prefix_no_regex" || r.MatchType == "exact" {
			return uriPath + strings.TrimPrefix(path, r.Pattern), rule, true
		}
		return path, rule, false
	case "apache":
		if uriPath == "" || r.Pattern == "" || !strings.HasPrefix(path, r.Pattern) {
			return path, rule, false
		}
		return strings.TrimSuffix(uriPath, "/") + "/" + strings.TrimPrefix(
			strings.TrimPrefix(path, r.Pattern), "/"), rule, true
	}
	return path, rule, false
}

// targetPath returns the path component of a proxy target, or "" when it has none.
// `http://api` has none; `http://api/` has "/".
func targetPath(target string) string {
	rest := target
	if _, after, ok := strings.Cut(target, "://"); ok {
		rest = after
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		return rest[i:]
	}
	return ""
}

func applyNginxRewrite(path, args string) (string, bool) {
	fields := strings.Fields(args)
	if len(fields) < 2 {
		return path, false
	}
	re, err := regexp.Compile(fields[0])
	if err != nil || !re.MatchString(path) {
		return path, false
	}
	// nginx replacements use $1..$9; Go's Expand syntax wants ${1}.
	repl := regexp.MustCompile(`\$(\d)`).ReplaceAllString(fields[1], "${$1}")
	return re.ReplaceAllString(path, repl), true
}

func applyApacheRewrite(path, args string) (string, bool) {
	fields := strings.Fields(args)
	if len(fields) < 2 {
		return path, false
	}
	re, err := regexp.Compile(fields[0])
	if err != nil || !re.MatchString(path) {
		return path, false
	}
	// `-` means "match, but do not substitute". It is how a RewriteRule that only
	// exists to carry a flag ([F], [E=…], [G]) is written, and treating it as a
	// substitution replaces the whole path with nothing.
	if fields[1] == "-" {
		return path, false
	}
	repl := regexp.MustCompile(`\$(\d)`).ReplaceAllString(fields[1], "${$1}")
	return re.ReplaceAllString(path, repl), true
}

func applyHAProxyPath(path, directive, args string) (string, bool) {
	fields := strings.Fields(args)
	if len(fields) == 0 || !strings.EqualFold(directive, "http-request") {
		return path, false
	}
	switch fields[0] {
	case "set-path":
		if len(fields) > 1 {
			return fields[1], true
		}
	case "replace-path":
		if len(fields) > 2 {
			re, err := regexp.Compile(fields[1])
			if err != nil || !re.MatchString(path) {
				return path, false
			}
			repl := regexp.MustCompile(`\\(\d)`).ReplaceAllString(fields[2], "${$1}")
			return re.ReplaceAllString(path, repl), true
		}
	}
	return path, false
}

func hasFlag(args string, flags ...string) bool {
	for _, f := range flags {
		if strings.Contains(args, f) {
			return true
		}
	}
	return false
}

func isStatic(r *Route, rules []HopRule, s *Site) bool {
	for _, hr := range rules {
		switch strings.ToLower(hr.Rule.Directive) {
		case "root", "alias", "documentroot", "try_files", "index", "sethandler":
			return true
		}
	}
	return r.TargetRaw == "" && s.DocumentRoot != ""
}

func unresolvableReason(r *Route) string {
	if strings.Contains(r.TargetRaw, "$") {
		return "the proxy target " + r.TargetRaw +
			" depends on a runtime variable, so the destination genuinely cannot be known from configuration"
	}
	if r.TargetRaw != "" {
		return "the proxy target " + r.TargetRaw + " does not resolve to a known upstream"
	}
	return "this route neither proxies nor serves files, so where the request goes is not determined by configuration"
}
