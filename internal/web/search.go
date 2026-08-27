package web

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// ---------------------------------------------------------------- search

// searchFacet is one narrowing option and how many hits it would keep. The count
// is shown because a facet with no number is a guess about what filtering costs.
type searchFacet struct {
	Value string
	Count int
}

// searchGroup is the unit of the results column: one file of one instance, with
// every hit in it. Grouping is the point — twenty hits in one file is one finding.
type searchGroup struct {
	InstanceID int64
	Instance   string
	// Node, because "nginx nginx.conf" is the display name of stock nginx on every
	// host: without it three results from three different nodes were three
	// identical rows and an operator could not tell which host to look at.
	Node       string
	Vendor     string
	SnapshotID int64
	FileID     int64
	Path       string
	Rules      []store.RuleHit
	Texts      []store.TextHit
}

const searchPageSize = 5

type searchData struct {
	Query     string
	MatchMode string // "substring" or "regex"
	Scope     string // "current" or "all"
	Rules     []store.RuleHit
	Configs   []store.TextHit
	Groups    []searchGroup
	// Facets are computed over the unfiltered hits, so ticking one never makes the
	// others vanish and an operator can always widen again.
	Vendors     []searchFacet
	Files       []searchFacet
	Clusters    []searchFacet
	SnapshotAge []searchFacet
	SelVend     []string
	SelFile     []string
	SelClust    []string
	SelAge      []string
	Matches     int
	FilesHit    int
	NodesHit    int
	// Instances is the total this query touched; From/To page over it.
	Instances             int
	From, To, Page, Pages int
	Duration              string // query execution time
	Err                   string
	RegexErr              string // regex compilation error
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	// Read parameters with defaults
	matchMode := q.Get("match")
	if matchMode == "" {
		matchMode = "substring"
	}
	scope := q.Get("scope")
	if scope == "" {
		scope = "current"
	}

	d := searchData{
		Query:     strings.TrimSpace(q.Get("q")),
		MatchMode: matchMode,
		Scope:     scope,
		SelVend:   nonEmpty(q["vendor"]),
		SelFile:   nonEmpty(q["file"]),
		SelClust:  nonEmpty(q["cluster"]),
		SelAge:    nonEmpty(q["age"]),
	}
	if d.Query == "" {
		s.render(w, r, "search.html", "Search", d)
		return
	}

	start := time.Now()

	// Validate regex if in regex mode
	var re *regexp.Regexp
	if d.MatchMode == "regex" {
		var err error
		re, err = regexp.Compile(d.Query)
		if err != nil {
			d.RegexErr = "Invalid regular expression: " + err.Error()
			s.render(w, r, "search.html", "Search", d)
			return
		}
	}

	// Search with scope parameter
	currentOnly := d.Scope != "all"
	rules, err := s.DB.SearchRulesScoped(ctx, d.Query, 200, currentOnly)
	if err != nil {
		d.Err = "rule search failed: " + err.Error()
	}
	texts, _ := s.DB.SearchConfigTextScoped(ctx, d.Query, 100, currentOnly)

	// Apply regex filtering if needed
	if re != nil {
		filteredRules := rules[:0]
		for _, r := range rules {
			if re.MatchString(r.Args) || re.MatchString(r.Raw) || re.MatchString(r.Directive) {
				filteredRules = append(filteredRules, r)
			}
		}
		rules = filteredRules

		filteredTexts := texts[:0]
		for _, t := range texts {
			if re.MatchString(t.Snippet) {
				filteredTexts = append(filteredTexts, t)
			}
		}
		texts = filteredTexts
	}

	d.Duration = time.Since(start).Round(time.Millisecond).String()

	// Query snapshot ages
	snapshotAges := map[int64]string{}
	ageRows, err := s.DB.R.QueryContext(ctx, `SELECT id, collected_at FROM snapshot`)
	if err == nil {
		defer ageRows.Close()
		now := time.Now()
		for ageRows.Next() {
			var id int64
			var collectedAt string
			if err := ageRows.Scan(&id, &collectedAt); err == nil {
				t, parseErr := time.Parse(time.RFC3339, collectedAt)
				if parseErr == nil {
					hours := now.Sub(t).Hours()
					switch {
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
	d.Vendors, d.Files, d.Clusters, d.SnapshotAge = facetList(vend), facetList(file), facetList(clust), facetList(age)

	keep := func(vendor, path, cluster string, snapshotID int64) bool {
		if cluster == "" {
			cluster = "(no cluster)"
		}
		ageOK := len(d.SelAge) == 0
		if !ageOK {
			if ageStr, ok := snapshotAges[snapshotID]; ok {
				ageOK = matchesFacet(d.SelAge, ageStr)
			}
		}
		return matchesFacet(d.SelVend, vendor) && matchesFacet(d.SelFile, path) &&
			matchesFacet(d.SelClust, cluster) && ageOK
	}
	// Group by (instance, file) in rank order, so the first box is the best hit.
	index := map[string]int{}
	for _, h := range rules {
		if !keep(h.Vendor, h.Path, h.Cluster, h.SnapshotID) {
			continue
		}
		d.Rules = append(d.Rules, h)
		i := groupFor(&d.Groups, index, searchGroup{InstanceID: h.InstanceID,
			Instance: h.Instance, Node: h.Node, Vendor: h.Vendor, SnapshotID: h.SnapshotID,
			FileID: h.FileID, Path: h.Path})
		d.Groups[i].Rules = append(d.Groups[i].Rules, h)
	}
	for _, h := range texts {
		if !keep(h.Vendor, h.Path, h.Cluster, h.SnapshotID) {
			continue
		}
		d.Configs = append(d.Configs, h)
		i := groupFor(&d.Groups, index, searchGroup{InstanceID: h.InstanceID,
			Instance: h.Instance, Node: h.Node, Vendor: h.Vendor, SnapshotID: h.SnapshotID,
			FileID: h.FileID, Path: h.Path})
		d.Groups[i].Texts = append(d.Groups[i].Texts, h)
	}
	d.Matches = len(d.Rules) + len(d.Configs)

	// Count distinct files and nodes
	fileSet := map[int64]bool{}
	nodeSet := map[string]bool{}
	for _, g := range d.Groups {
		fileSet[g.FileID] = true
		if g.Node != "" {
			nodeSet[g.Node] = true
		}
	}
	d.FilesHit = len(fileSet)
	d.NodesHit = len(nodeSet)

	// Paging is by instance, not by hit: an instance split across two pages would
	// make "17 instances mention this" unreadable.
	var order []int64
	seen := map[int64]bool{}
	for _, g := range d.Groups {
		if !seen[g.InstanceID] {
			seen[g.InstanceID] = true
			order = append(order, g.InstanceID)
		}
	}
	d.Instances = len(order)
	d.Page, _ = strconv.Atoi(q.Get("page"))
	if d.Page < 1 {
		d.Page = 1
	}
	d.Pages = max((d.Instances+searchPageSize-1)/searchPageSize, 1)
	if d.Page > d.Pages {
		d.Page = d.Pages
	}
	offset := (d.Page - 1) * searchPageSize
	end := min(offset+searchPageSize, d.Instances)
	if d.Instances > 0 {
		d.From, d.To = offset+1, end
	}
	onPage := map[int64]bool{}
	for _, id := range order[offset:end] {
		onPage[id] = true
	}
	kept := d.Groups[:0]
	for _, g := range d.Groups {
		if onPage[g.InstanceID] {
			kept = append(kept, g)
		}
	}
	d.Groups = kept
	s.render(w, r, "search.html", "Search", d)
}

// groupFor returns the index of the (instance, file) group, appending it in
// first-seen order so rank survives the grouping.
func groupFor(groups *[]searchGroup, index map[string]int, g searchGroup) int {
	key := fmt.Sprintf("%d:%d:%s", g.InstanceID, g.FileID, g.Path)
	if i, ok := index[key]; ok {
		return i
	}
	*groups = append(*groups, g)
	index[key] = len(*groups) - 1
	return len(*groups) - 1
}

func facetList(counts map[string]int) []searchFacet {
	out := make([]searchFacet, 0, len(counts))
	for v, n := range counts {
		out = append(out, searchFacet{v, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// matchesFacet treats an empty selection as "everything". Nothing ticked is not
// the same as every box ticked, and the screen says so in words.
func matchesFacet(selected []string, value string) bool {
	if len(selected) == 0 {
		return true
	}
	return has(selected, value)
}
