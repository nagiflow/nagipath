package api

import (
	"context"
	"regexp"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// searchService implements pb.SearchServiceServer
// (proto/nagipath/api/v1/search.proto). Ports getSearch (formerly
// search.go); facetList, matchesFacet, toPBRuleHit, toPBTextHit, groupFor
// and searchPageSize stay in search.go, the only other caller.
type searchService struct {
	pb.UnimplementedSearchServiceServer
	s *Server
}

func (c *searchService) GetSearch(ctx context.Context, req *pb.GetSearchRequest) (*pb.SearchResponse, error) {
	s := c.s
	matchMode := req.Match
	if matchMode == "" {
		matchMode = "substring"
	}
	scope := req.Scope
	if scope == "" {
		scope = "current"
	}

	resp := &pb.SearchResponse{
		Query: strings.TrimSpace(req.Q), MatchMode: matchMode, Scope: scope,
		SelVendor: nonEmpty(req.Vendor), SelFile: nonEmpty(req.File),
		SelCluster: nonEmpty(req.Cluster), SelAge: nonEmpty(req.Age),
	}
	if resp.Query == "" {
		return resp, nil
	}

	start := time.Now()

	var re *regexp.Regexp
	if resp.MatchMode == "regex" {
		var err error
		re, err = regexp.Compile(resp.Query)
		if err != nil {
			resp.RegexError = "Invalid regular expression: " + err.Error()
			return resp, nil
		}
	}

	currentOnly := resp.Scope != "all"
	rules, err := s.DB.SearchRulesScoped(ctx, resp.Query, 200, currentOnly)
	if err != nil {
		resp.Error = "rule search failed: " + err.Error()
	}
	texts, _ := s.DB.SearchConfigTextScoped(ctx, resp.Query, 100, currentOnly)

	if re != nil {
		filteredRules := rules[:0]
		for _, hit := range rules {
			if re.MatchString(hit.Args) || re.MatchString(hit.Raw) || re.MatchString(hit.Directive) {
				filteredRules = append(filteredRules, hit)
			}
		}
		rules = filteredRules

		filteredTexts := texts[:0]
		for _, hit := range texts {
			if re.MatchString(hit.Snippet) {
				filteredTexts = append(filteredTexts, hit)
			}
		}
		texts = filteredTexts
	}

	resp.Duration = time.Since(start).Round(time.Millisecond).String()

	snapshotAges := map[int64]string{}
	ageRows, err := s.DB.R.QueryContext(ctx, `SELECT id, collected_at FROM snapshot`)
	if err == nil {
		defer ageRows.Close()
		now := time.Now()
		for ageRows.Next() {
			var id int64
			var collectedAt string
			if err := ageRows.Scan(&id, &collectedAt); err == nil {
				if t, parseErr := time.Parse(time.RFC3339, collectedAt); parseErr == nil {
					switch hours := now.Sub(t).Hours(); {
					case hours < 6:
						snapshotAges[id] = "< 6h"
					case hours < 24:
						snapshotAges[id] = "6–24h"
					default:
						snapshotAges[id] = "> 24h"
					}
				}
			}
		}
		_ = ageRows.Err()
	}

	vend, file, clust, age := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	count := func(vendor, path, cluster string, snapshotID int64) {
		vend[vendor]++
		file[path]++
		if cluster == "" {
			cluster = "(no cluster)"
		}
		clust[cluster]++
		if ageStr, ok := snapshotAges[snapshotID]; ok {
			age[ageStr]++
		}
	}
	for _, h := range rules {
		count(h.Vendor, h.Path, h.Cluster, h.SnapshotID)
	}
	for _, h := range texts {
		count(h.Vendor, h.Path, h.Cluster, h.SnapshotID)
	}
	resp.Vendors, resp.Files, resp.Clusters, resp.SnapshotAge = facetList(vend), facetList(file), facetList(clust), facetList(age)

	keep := func(vendor, path, cluster string, snapshotID int64) bool {
		if cluster == "" {
			cluster = "(no cluster)"
		}
		ageOK := len(resp.SelAge) == 0
		if !ageOK {
			if ageStr, ok := snapshotAges[snapshotID]; ok {
				ageOK = matchesFacet(resp.SelAge, ageStr)
			}
		}
		return matchesFacet(resp.SelVendor, vendor) && matchesFacet(resp.SelFile, path) &&
			matchesFacet(resp.SelCluster, cluster) && ageOK
	}

	var groups []*pb.SearchGroup
	index := map[string]int{}
	var matchedRules []store.RuleHit
	for _, h := range rules {
		if !keep(h.Vendor, h.Path, h.Cluster, h.SnapshotID) {
			continue
		}
		matchedRules = append(matchedRules, h)
		i := groupFor(&groups, index, h.InstanceID, h.FileID, h.Instance, h.Node, h.Vendor, h.Path, h.SnapshotID)
		groups[i].Rules = append(groups[i].Rules, toPBRuleHit(h))
	}
	var matchedTexts []store.TextHit
	for _, h := range texts {
		if !keep(h.Vendor, h.Path, h.Cluster, h.SnapshotID) {
			continue
		}
		matchedTexts = append(matchedTexts, h)
		i := groupFor(&groups, index, h.InstanceID, h.FileID, h.Instance, h.Node, h.Vendor, h.Path, h.SnapshotID)
		groups[i].Texts = append(groups[i].Texts, toPBTextHit(h))
	}
	resp.Matches = int32(len(matchedRules) + len(matchedTexts))

	fileSet := map[int64]bool{}
	nodeSet := map[string]bool{}
	for _, g := range groups {
		fileSet[g.FileId] = true
		if g.Node != "" {
			nodeSet[g.Node] = true
		}
	}
	resp.FilesHit, resp.NodesHit = int32(len(fileSet)), int32(len(nodeSet))

	var order []int64
	seen := map[int64]bool{}
	for _, g := range groups {
		if !seen[g.InstanceId] {
			seen[g.InstanceId] = true
			order = append(order, g.InstanceId)
		}
	}
	resp.Instances = int32(len(order))
	page := int(req.Page)
	if page < 1 {
		page = 1
	}
	pages := max((len(order)+searchPageSize-1)/searchPageSize, 1)
	if page > pages {
		page = pages
	}
	offset := (page - 1) * searchPageSize
	end := min(offset+searchPageSize, len(order))
	resp.Page, resp.Pages = int32(page), int32(pages)
	if len(order) > 0 {
		resp.From, resp.To = int32(offset+1), int32(end)
	}
	onPage := map[int64]bool{}
	for _, id := range order[offset:end] {
		onPage[id] = true
	}
	for _, g := range groups {
		if onPage[g.InstanceId] {
			resp.Groups = append(resp.Groups, g)
		}
	}
	return resp, nil
}
