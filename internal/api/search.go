package api

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

const searchPageSize = 5

func facetList(counts map[string]int) []*pb.SearchFacet {
	out := make([]*pb.SearchFacet, 0, len(counts))
	for v, n := range counts {
		out = append(out, &pb.SearchFacet{Value: v, Count: int32(n)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// matchesFacet treats an empty selection as "everything". Nothing ticked is
// not the same as every box ticked, and the screen says so in words.
func matchesFacet(selected []string, value string) bool {
	if len(selected) == 0 {
		return true
	}
	for _, s := range selected {
		if s == value {
			return true
		}
	}
	return false
}

func toPBRuleHit(h store.RuleHit) *pb.RuleHitPB {
	return &pb.RuleHitPB{RuleId: h.RuleID, InstanceId: h.InstanceID, SnapshotId: h.SnapshotID, FileId: h.FileID,
		Instance: h.Instance, Vendor: h.Vendor, Node: h.Node, Cluster: h.Cluster, Directive: h.Directive,
		ActionClass: h.ActionClass, Args: h.Args, Raw: h.Raw, Path: h.Path, ByteStart: int32(h.ByteStart), Shadowed: h.Shadowed}
}

func toPBTextHit(h store.TextHit) *pb.TextHitPB {
	return &pb.TextHitPB{FileId: h.FileID, SnapshotId: h.SnapshotID, InstanceId: h.InstanceID,
		Instance: h.Instance, Vendor: h.Vendor, Node: h.Node, Cluster: h.Cluster, Path: h.Path, Snippet: h.Snippet}
}

// groupFor returns the index of the (instance, file) group, appending it in
// first-seen order so rank survives the grouping.
func groupFor(groups *[]*pb.SearchGroup, index map[string]int, instanceID, fileID int64, instance, node, vendor, path string, snapshotID int64) int {
	key := fmt.Sprintf("%d:%d:%s", instanceID, fileID, path)
	if i, ok := index[key]; ok {
		return i
	}
	*groups = append(*groups, &pb.SearchGroup{InstanceId: instanceID, Instance: instance, Node: node,
		Vendor: vendor, SnapshotId: snapshotID, FileId: fileID, Path: path})
	index[key] = len(*groups) - 1
	return len(*groups) - 1
}

// getSearch ports internal/web/search.go's search(): a structured rule search
// plus a raw config-text search, grouped by (instance, file), with facets
// computed over the unfiltered hits so ticking one filter never hides the
// options for widening again.
func (s *Server) getSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	matchMode := q.Get("match")
	if matchMode == "" {
		matchMode = "substring"
	}
	scope := q.Get("scope")
	if scope == "" {
		scope = "current"
	}

	resp := &pb.SearchResponse{
		Query: strings.TrimSpace(q.Get("q")), MatchMode: matchMode, Scope: scope,
		SelVendor: nonEmpty(q["vendor"]), SelFile: nonEmpty(q["file"]),
		SelCluster: nonEmpty(q["cluster"]), SelAge: nonEmpty(q["age"]),
	}
	if resp.Query == "" {
		writeProto(w, http.StatusOK, resp)
		return
	}

	start := time.Now()

	var re *regexp.Regexp
	if resp.MatchMode == "regex" {
		var err error
		re, err = regexp.Compile(resp.Query)
		if err != nil {
			resp.RegexError = "Invalid regular expression: " + err.Error()
			writeProto(w, http.StatusOK, resp)
			return
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
	page, _ := strconv.Atoi(q.Get("page"))
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
	writeProto(w, http.StatusOK, resp)
}
