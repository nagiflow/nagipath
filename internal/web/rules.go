package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/trace"
)

// ---------------------------------------------------------------- rule lookup

// ruleGroup holds results grouped by node, with identical rule sets collapsed.
type ruleGroup struct {
	NodeName string
	Results  []trace.LookupResult
	Hash     string
	Count    int // number of nodes with identical rules
}

// ruleWithSummary wraps a LookupRule with its plain-language summary.
type ruleWithSummary struct {
	trace.LookupRule
	Summary string
}

type ruleFacet struct {
	Value string
	Count int
}

type rulesData struct {
	Query   trace.LookupQuery
	URL     string // Query rendered back the way it would be pasted, for the input's value
	Results []trace.LookupResult
	Classes []string
	Vendors []string
	// Answered and Silent split the results: instances with candidates, and
	// instances that were considered and had none.
	Answered []trace.LookupResult
	Silent   []trace.LookupResult
	// Groups holds results grouped by node with collapsing
	Groups       []ruleGroup
	Rules        int
	Nodes        int
	Files        int
	Asked        bool
	Empty        bool
	Page         int
	Pages        int
	From, To     int
	VendorFacets []ruleFacet
	ClassFacets  []ruleFacet
	// RulesUnfiltered is what the "no rules match the filters" panel quotes. Rules
	// is zero by definition on that screen, so it cannot be the number shown.
	RulesUnfiltered int
}

// ruleTally is one pass over a lookup's results: the split into instances with
// candidates and instances without, plus everything the header and the facet
// panel count.
type ruleTally struct {
	answered, silent []trace.LookupResult
	rules            int
	nodes, files     map[string]bool
	vendors, classes map[string]int
}

func tallyRules(results []trace.LookupResult) ruleTally {
	t := ruleTally{
		nodes: map[string]bool{}, files: map[string]bool{},
		vendors: map[string]int{}, classes: map[string]int{},
	}
	for _, res := range results {
		if len(res.Rules) == 0 {
			t.silent = append(t.silent, res)
			continue
		}
		t.answered = append(t.answered, res)
		t.rules += len(res.Rules)
		if res.Inst != nil {
			t.vendors[res.Inst.Vendor]++
			if res.Inst.NodeName != "" {
				t.nodes[res.Inst.NodeName] = true
			}
		}
		for _, lr := range res.Rules {
			if lr.Rule.ActionClass != "" {
				t.classes[lr.Rule.ActionClass]++
			}
			if lr.Rule.Path != "" {
				t.files[lr.Rule.Path] = true
			}
		}
	}
	return t
}

// sortedFacets orders a facet column by count, then alphabetically so a redraw
// never reshuffles two filters that happen to tie.
func sortedFacets(counts map[string]int) []ruleFacet {
	out := make([]ruleFacet, 0, len(counts))
	for v, c := range counts {
		out = append(out, ruleFacet{v, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// rules answers "which rules are in effect for this context path" without
// following the request anywhere. It is the same site and route selection the
// trace engine uses, stopped after one hop.
func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	d := rulesData{Classes: trace.ActionClasses}

	// One pasted URL, like the Trace page — scheme, port and path are read off
	// it, and a bare host defaults to https. Unlike Trace, a bare path (starting
	// with "/") is also accepted here: Trace walks one specific hop chain and
	// needs a host to start from, but a rule lookup can instead search every
	// site in the fleet for a route that claims that path — useful when the
	// question is "who serves /api/v2" rather than "what handles this request".
	// The separate scheme/hostname/path/port parameters still work unchanged,
	// because the "Rule lookup" button on the instance page, and older
	// bookmarks, build a link that way.
	raw := strings.TrimSpace(q.Get("url"))
	var tq trace.Query
	if strings.HasPrefix(raw, "/") {
		tq = trace.Query{Path: raw}
	} else {
		var err error
		tq, err = parseTarget(raw)
		if err != nil {
			redirect(w, r, "/rules", "", err.Error())
			return
		}
	}
	legacyPath := strings.TrimSpace(q.Get("path"))
	if tq.Hostname == "" && tq.Path == "" {
		tq = trace.Query{
			Scheme:   q.Get("scheme"),
			Hostname: strings.TrimSpace(q.Get("hostname")),
			Path:     legacyPath,
		}
		if p, err := strconv.Atoi(q.Get("port")); err == nil {
			tq.Port = p
		}
	}
	d.Query = trace.LookupQuery{
		Scheme: tq.Scheme, Hostname: tq.Hostname, Path: tq.Path, Port: tq.Port,
		Classes: nonEmpty(q["class"]), Vendors: nonEmpty(q["vendor"]),
	}.Normalise()
	switch {
	case d.Query.Hostname != "":
		d.URL = targetURL(trace.Query{Scheme: d.Query.Scheme, Hostname: d.Query.Hostname,
			Path: d.Query.Path, Port: d.Query.Port})
	case raw != "":
		d.URL = d.Query.Path
	}

	instances, err := s.DB.Instances(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Empty = len(instances) == 0
	seen := map[string]bool{}
	for _, in := range instances {
		if !seen[in.Vendor] {
			seen[in.Vendor] = true
			d.Vendors = append(d.Vendors, in.Vendor)
		}
	}
	sort.Strings(d.Vendors)

	asked := raw != "" || d.Query.Hostname != "" || legacyPath != ""
	if !asked || d.Empty {
		s.render(w, r, "rules.html", "Rule lookup", d)
		return
	}
	d.Asked = true
	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Results = trace.Lookup(top, d.Query)

	shown := tallyRules(d.Results)
	d.Answered, d.Silent, d.Rules = shown.answered, shown.silent, shown.rules
	d.Nodes, d.Files = len(shown.nodes), len(shown.files)

	// Facets come from the lookup with the filters taken off, not from the filtered
	// results. Computed from d.Results, ticking one class deleted every other
	// checkbox from the panel and left its own count reading as the whole fleet's:
	// the only way to widen again was to edit the URL by hand, which is why the
	// filter read as broken. The counts also have to mean "how many rules this
	// filter would leave", which is what the panel says they mean.
	facets := shown
	if len(d.Query.Classes) > 0 || len(d.Query.Vendors) > 0 {
		wide := d.Query
		wide.Classes, wide.Vendors = nil, nil
		facets = tallyRules(trace.Lookup(top, wide))
	}
	d.VendorFacets = sortedFacets(facets.vendors)
	d.ClassFacets = sortedFacets(facets.classes)
	d.RulesUnfiltered = facets.rules

	// Group by node and collapse identical rule sets
	d.Groups = groupByNode(d.Answered)

	// Pagination (by node group, not by instance)
	const pageSize = 20
	d.Page, _ = strconv.Atoi(q.Get("page"))
	if d.Page < 1 {
		d.Page = 1
	}
	d.Pages = max((len(d.Groups)+pageSize-1)/pageSize, 1)
	if d.Page > d.Pages {
		d.Page = d.Pages
	}
	start := (d.Page - 1) * pageSize
	end := min(start+pageSize, len(d.Groups))
	if len(d.Groups) > 0 {
		d.From, d.To = start+1, end
		d.Groups = d.Groups[start:end]
	}

	if q.Get("export") == "csv" {
		rows := [][]string{{"instance", "node", "vendor", "site", "route", "scope", "directive", "arguments", "class", "source", "byte_start"}}
		for _, result := range d.Answered {
			site, route := "", ""
			if result.Site != nil {
				site = result.Site.PrimaryName
			}
			if result.Route != nil {
				route = result.Route.Pattern
			}
			for _, item := range result.Rules {
				rows = append(rows, []string{result.Inst.DisplayName, result.Inst.NodeName,
					result.Inst.Vendor, site, route, item.Scope, item.Rule.Directive,
					item.Rule.Args, item.Rule.ActionClass, item.Rule.Path,
					strconv.Itoa(item.Rule.ByteStart)})
			}
		}
		s.writeCSV(w, r, "rule-lookup", rows)
		return
	}
	s.render(w, r, "rules.html", "Rule lookup", d)
}

// groupByNode groups results by node and collapses identical rule sets.
func groupByNode(results []trace.LookupResult) []ruleGroup {
	// Hash each result's rule set
	hashes := make(map[string][]trace.LookupResult)

	for _, res := range results {
		h := hashRuleSet(res)
		hashes[h] = append(hashes[h], res)
	}

	// Build groups, collapsing identical ones
	var groups []ruleGroup
	for hash, rs := range hashes {
		if len(rs) == 0 {
			continue
		}
		g := ruleGroup{
			Hash:    hash,
			Results: rs,
			Count:   len(rs),
		}
		// Use first result's node as the representative
		if rs[0].Inst != nil {
			g.NodeName = rs[0].Inst.NodeName
		}
		groups = append(groups, g)
	}

	// Sort by node name for stable ordering
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].NodeName < groups[j].NodeName
	})

	return groups
}

// hashRuleSet creates a stable hash of a result's rule set for collapsing.
func hashRuleSet(res trace.LookupResult) string {
	// Serialize rules to JSON for hashing
	type ruleKey struct {
		Directive string
		Args      string
		Class     string
		Scope     string
		Shadowed  bool
	}

	var keys []ruleKey
	for _, lr := range res.Rules {
		keys = append(keys, ruleKey{
			Directive: lr.Rule.Directive,
			Args:      lr.Rule.Args,
			Class:     lr.Rule.ActionClass,
			Scope:     lr.Scope,
			Shadowed:  lr.Shadowed,
		})
	}

	// Also include site and route info
	site, route := "", ""
	if res.Site != nil {
		site = res.Site.PrimaryName
	}
	if res.Route != nil {
		route = res.Route.Pattern
	}

	data, _ := json.Marshal(struct {
		Site  string
		Route string
		Rules []ruleKey
	}{site, route, keys})

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func nonEmpty(list []string) []string {
	var out []string
	for _, s := range list {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
