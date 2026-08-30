package api

import (
	"context"
	"sort"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ruleService implements pb.RuleServiceServer
// (proto/nagipath/api/v1/rules.proto). Ports getRules (formerly rules.go);
// ruleTally, tallyRules, sortedRuleFacets, nonEmpty, toPBLookupResult,
// hashRuleSet and groupByNode stay in rules.go since getRulesCSV still
// needs them too.
type ruleService struct {
	pb.UnimplementedRuleServiceServer
	s *Server
}

func (c *ruleService) GetRules(ctx context.Context, req *pb.GetRulesRequest) (*pb.RulesResponse, error) {
	s := c.s
	resp := &pb.RulesResponse{Classes: trace.ActionClasses}

	raw := strings.TrimSpace(req.Url)
	var tq trace.Query
	if strings.HasPrefix(raw, "/") {
		tq = trace.Query{Path: raw}
	} else {
		var err error
		tq, err = parseTarget(raw)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	legacyPath := strings.TrimSpace(req.Path)
	if tq.Hostname == "" && tq.Path == "" {
		tq = trace.Query{Scheme: req.Scheme, Hostname: strings.TrimSpace(req.Hostname), Path: legacyPath, Port: int(req.Port)}
	}
	lq := trace.LookupQuery{Scheme: tq.Scheme, Hostname: tq.Hostname, Path: tq.Path, Port: tq.Port,
		Classes: nonEmpty(req.Class), Vendors: nonEmpty(req.Vendor)}.Normalise()
	resp.Scheme, resp.Hostname, resp.Path, resp.Port = lq.Scheme, lq.Hostname, lq.Path, int32(lq.Port)
	resp.ClassesSelected, resp.VendorsSelected = lq.Classes, lq.Vendors
	switch {
	case lq.Hostname != "":
		resp.Url = targetURL(trace.Query{Scheme: lq.Scheme, Hostname: lq.Hostname, Path: lq.Path, Port: lq.Port})
	case raw != "":
		resp.Url = lq.Path
	}

	instances, err := s.DB.Instances(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp.Empty = len(instances) == 0
	seen := map[string]bool{}
	for _, in := range instances {
		if !seen[in.Vendor] {
			seen[in.Vendor] = true
			resp.Vendors = append(resp.Vendors, in.Vendor)
		}
	}
	sort.Strings(resp.Vendors)

	asked := raw != "" || lq.Hostname != "" || legacyPath != ""
	if !asked || resp.Empty {
		return resp, nil
	}
	resp.Asked = true
	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	results := trace.Lookup(top, lq)

	shown := tallyRules(results)
	resp.Rules, resp.Nodes, resp.Files = int32(shown.rules), int32(len(shown.nodes)), int32(len(shown.files))

	facets := shown
	if len(lq.Classes) > 0 || len(lq.Vendors) > 0 {
		wide := lq
		wide.Classes, wide.Vendors = nil, nil
		facets = tallyRules(trace.Lookup(top, wide))
	}
	resp.VendorFacets = sortedRuleFacets(facets.vendors)
	resp.ClassFacets = sortedRuleFacets(facets.classes)
	resp.RulesUnfiltered = int32(facets.rules)

	for _, res := range shown.silent {
		resp.Silent = append(resp.Silent, toPBLookupResult(res))
	}

	groups := groupByNode(shown.answered)

	const pageSize = 20
	page := int(req.Page)
	if page < 1 {
		page = 1
	}
	pages := max((len(groups)+pageSize-1)/pageSize, 1)
	if page > pages {
		page = pages
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, len(groups))
	resp.Page, resp.Pages = int32(page), int32(pages)
	if len(groups) > 0 {
		resp.From, resp.To = int32(start+1), int32(end)
		groups = groups[start:end]
	}
	resp.Groups = groups

	return resp, nil
}
