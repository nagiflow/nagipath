package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/trace"
)

// getRules moved to ruleservice.go as RuleService's GetRules RPC
// (docs/adr/0018, proto/nagipath/api/v1/rules.proto). Everything below stays
// here: shared with ruleservice.go and getRulesCSV.

// ruleTally is one pass over a lookup's results: the split into instances with
// candidates and instances without, plus everything the header and the facet
// panel count. Ported from internal/web/rules.go's tallyRules.
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

func sortedRuleFacets(counts map[string]int) []*pb.RuleFacet {
	out := make([]*pb.RuleFacet, 0, len(counts))
	for v, c := range counts {
		out = append(out, &pb.RuleFacet{Value: v, Count: int32(c)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
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

func toPBLookupResult(res trace.LookupResult) *pb.LookupResultPB {
	r := &pb.LookupResultPB{MatchedBy: res.MatchedBy, Precedence: res.Precedence,
		Degraded: res.Degraded, Reason: res.Reason}
	if res.Inst != nil {
		r.InstId, r.InstDisplayName, r.InstNodeName, r.InstVendor, r.InstDegraded =
			res.Inst.ID, res.Inst.DisplayName, res.Inst.NodeName, res.Inst.Vendor, res.Inst.Degraded
		r.NodeId, r.ClusterName = res.Inst.NodeID, res.Inst.ClusterName
	}
	if res.Listener != nil {
		r.ListenerAddress, r.ListenerPort, r.ListenerTls = res.Listener.Address, int32(res.Listener.Port), res.Listener.TLS
	}
	if res.Site != nil {
		r.SiteName = res.Site.PrimaryName
	}
	if res.Route != nil {
		r.RoutePattern = res.Route.Pattern
	}
	for _, lr := range res.Rules {
		item := &pb.LookupRulePB{Ordinal: int32(lr.Ordinal), Scope: lr.Scope, Inherited: lr.Inherited,
			Shadowed: lr.Shadowed, ShadowedBy: lr.ShadowedBy}
		if lr.Rule != nil {
			item.Directive, item.ActionClass, item.Args = lr.Rule.Directive, lr.Rule.ActionClass, lr.Rule.Args
			item.Path, item.ByteStart = lr.Rule.Path, int32(lr.Rule.ByteStart)
			item.FileId, item.SnapshotId = lr.Rule.FileID, lr.Rule.SnapshotID
		}
		r.Rules = append(r.Rules, item)
	}
	for _, b := range res.Undetermined {
		r.Undetermined = append(r.Undetermined, &pb.LookupBranchPB{HopOrdinal: int32(b.HopOrdinal), Raw: b.Raw, Reason: b.Reason})
	}
	return r
}

// hashRuleSet creates a stable hash of a result's rule set for collapsing
// identical node results together. Ported from internal/web/rules.go.
func hashRuleSet(res trace.LookupResult) string {
	type ruleKey struct {
		Directive string
		Args      string
		Class     string
		Scope     string
		Shadowed  bool
	}
	var keys []ruleKey
	for _, lr := range res.Rules {
		keys = append(keys, ruleKey{Directive: lr.Rule.Directive, Args: lr.Rule.Args,
			Class: lr.Rule.ActionClass, Scope: lr.Scope, Shadowed: lr.Shadowed})
	}
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

func groupByNode(results []trace.LookupResult) []*pb.RuleGroup {
	hashes := make(map[string][]trace.LookupResult)
	for _, res := range results {
		h := hashRuleSet(res)
		hashes[h] = append(hashes[h], res)
	}
	var groups []*pb.RuleGroup
	for hash, rs := range hashes {
		if len(rs) == 0 {
			continue
		}
		g := &pb.RuleGroup{Hash: hash, Count: int32(len(rs))}
		if rs[0].Inst != nil {
			g.NodeName = rs[0].Inst.NodeName
		}
		for _, res := range rs {
			g.Results = append(g.Results, toPBLookupResult(res))
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].NodeName < groups[j].NodeName })
	return groups
}

// getRulesCSV: GET /rules?export=csv is a formatted download, not RPC-shaped
// data (gateway.go's gatewayOrCSV, wired in api.go). Rebuilds the same
// lookup GetRules does — CSV needs the unfiltered shown.answered rather than
// a paginated *pb.RulesResponse, so it isn't worth routing through the RPC
// just to throw most of the response away.
func (s *Server) getRulesCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	raw := strings.TrimSpace(q.Get("url"))
	var tq trace.Query
	if strings.HasPrefix(raw, "/") {
		tq = trace.Query{Path: raw}
	} else {
		var err error
		tq, err = parseTarget(raw)
		if err != nil {
			apiError(w, http.StatusUnprocessableEntity, "invalid_url", err.Error())
			return
		}
	}
	legacyPath := strings.TrimSpace(q.Get("path"))
	if tq.Hostname == "" && tq.Path == "" {
		tq = trace.Query{Scheme: q.Get("scheme"), Hostname: strings.TrimSpace(q.Get("hostname")), Path: legacyPath}
		if p, err := strconv.Atoi(q.Get("port")); err == nil {
			tq.Port = p
		}
	}
	lq := trace.LookupQuery{Scheme: tq.Scheme, Hostname: tq.Hostname, Path: tq.Path, Port: tq.Port,
		Classes: nonEmpty(q["class"]), Vendors: nonEmpty(q["vendor"])}.Normalise()

	asked := raw != "" || lq.Hostname != "" || legacyPath != ""
	var answered []trace.LookupResult
	if asked {
		top, err := trace.Load(ctx, s.DB)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
		}
		answered = tallyRules(trace.Lookup(top, lq)).answered
	}

	rows := [][]string{{"instance", "node", "vendor", "site", "route", "scope", "directive", "arguments", "class", "source", "byte_start"}}
	for _, result := range answered {
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
}
