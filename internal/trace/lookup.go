package trace

import (
	"sort"
	"strconv"
	"strings"
)

// Rule lookup answers "which rules are in effect for this host and path, and in
// what order" without following the request anywhere. It is deliberately the same
// site and route selection the trace engine uses, stopped after one hop: if the
// two disagreed, one of them would be lying, and there would be no way to tell
// which (rule_lookup_by_path.md §1).

type LookupQuery struct {
	Hostname string
	Path     string
	Port     int
	Scheme   string
	// Classes and Vendors narrow the answer. Empty means every class and vendor:
	// a filter that defaults to something is a filter that hides things.
	Classes []string
	Vendors []string
}

func (q LookupQuery) Normalise() LookupQuery {
	if q.Scheme == "" {
		q.Scheme = "https"
	}
	if q.Path == "" {
		q.Path = "/"
	}
	if q.Port == 0 {
		q.Port = defaultPort(q.Scheme)
	}
	q.Hostname = strings.ToLower(strings.TrimSpace(q.Hostname))
	return q
}

// LookupRule is one rule in evaluation order, with the scope it came from. A rule
// inherited from an outer scope says so: "where is this set" is the question the
// operator actually has.
type LookupRule struct {
	Ordinal    int
	Rule       *Rule
	Scope      string
	Inherited  bool
	Shadowed   bool
	ShadowedBy string
}

// LookupResult is one Instance's answer. An Instance with no candidates is still
// a result: "this one does not serve that host" is information, and omitting it
// leaves the operator wondering whether it was checked.
type LookupResult struct {
	Inst         *Inst
	Listener     *Listener
	Site         *Site
	MatchedBy    string
	Route        *Route
	Precedence   string
	Degraded     bool
	Reason       string
	Rules        []LookupRule
	Undetermined []Branch
}

// Confidence is always the weakest word that is true. Nothing here was observed
// happening, so nothing here is more than a candidate; only Probe evidence can
// raise a rule above this.
func (r LookupResult) Confidence() string {
	if r.Degraded || (r.Inst != nil && r.Inst.ParseState != "parsed") {
		return "partial"
	}
	return "candidate"
}

// Lookup evaluates the query against every current Snapshot in the Topology.
func Lookup(top *Topology, q LookupQuery) []LookupResult {
	q = q.Normalise()
	tls := q.Scheme == "https"
	classes := set(q.Classes)
	vendors := set(q.Vendors)

	var out []LookupResult
	for _, in := range top.Instances {
		if len(vendors) > 0 && !vendors[in.Vendor] {
			continue
		}
		res := LookupResult{Inst: in, Degraded: in.Degraded}
		if in.ParseState != "parsed" {
			// A snapshot nagipath could not parse is reported as unanswerable, not
			// as an instance with no rules.
			res.Reason = "this snapshot did not parse (" + in.ParseState + "), so nagipath will not claim what is in effect here"
			out = append(out, res)
			continue
		}

		listener, site, matchedBy := selectSite(in, q.Hostname, q.Port, tls)
		res.Listener, res.Site, res.MatchedBy = listener, site, matchedBy
		if site == nil {
			if listener == nil {
				res.Reason = "nothing on this instance listens on port " + strconv.Itoa(q.Port) + " " + tlsWord(tls)
			} else {
				res.Reason = "something listens, but no site here claims that hostname"
			}
			out = append(out, res)
			continue
		}

		route, precedence, branches := selectRoute(in.Vendor, site, q.Path)
		res.Route, res.Precedence, res.Undetermined = route, precedence, branches
		applied, shadowed := effectiveRules(in, site, route)

		n := 0
		for _, hr := range applied {
			if len(classes) > 0 && !classes[hr.Rule.ActionClass] {
				continue
			}
			n++
			res.Rules = append(res.Rules, LookupRule{
				Ordinal: n, Rule: hr.Rule, Scope: hr.Scope, Inherited: hr.Inherited,
			})
		}
		// Shadowed rules go last and carry the inner directive that discarded them.
		// They are the finding an operator most often misses by reading the file.
		for _, hr := range shadowed {
			if len(classes) > 0 && !classes[hr.Rule.ActionClass] {
				continue
			}
			res.Rules = append(res.Rules, LookupRule{
				Rule: hr.Rule, Scope: hr.Scope, Inherited: hr.Inherited,
				Shadowed: true, ShadowedBy: hr.Rule.ShadowedBy,
			})
		}
		if route == nil && len(res.Rules) == 0 {
			res.Reason = "the site has no route for that path and no catch-all"
		}
		out = append(out, res)
	}

	// Instances with an answer first; the rest keep their place in the list so an
	// operator can see they were considered.
	sort.SliceStable(out, func(i, j int) bool {
		return len(out[i].Rules) > 0 && len(out[j].Rules) == 0
	})
	return out
}

// ActionClasses is the closed set, in the order the filter lists them.
var ActionClasses = []string{"match", "rewrite", "redirect", "header", "auth",
	"cache", "rate_limit", "proxy", "access_control", "other"}

func set(list []string) map[string]bool {
	if len(list) == 0 {
		return nil
	}
	m := make(map[string]bool, len(list))
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = true
		}
	}
	return m
}
