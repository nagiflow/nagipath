package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// Version is stamped by main and used in the Probe User-Agent, so a request in a
// customer's access log can be attributed to a specific nagipath build.
var Version = "dev"

// ---------------------------------------------------------------- drift

type driftGroup struct {
	Kind      string
	Key       string
	Change    string
	Instances []string
	FirstSeen string
	Findings  []store.DriftFinding
}

// clusterGroup is one cluster header on the Drift work queue.
type clusterGroup struct {
	ClusterName    string
	BaselineName   string
	NodesWithDrift int
	TotalNodes     int
	Instances      []instanceWithDrift
}

// instanceWithDrift is one row in the Nodes off baseline table.
type instanceWithDrift struct {
	ID              int64
	DisplayName     string
	NodeDisplayName string
	DivergenceCount int
	ObjectBreakdown string
	FirstSeen       string
}

type driftData struct {
	Clusters                []store.Cluster
	Cluster                 store.Cluster
	ClusterID               int64
	Baseline                string
	Baselines               []struct{ Kind, Label string }
	Members                 []store.Instance
	AllScope                bool
	Scope                   string
	ScopeFilter             string
	Counts                  map[int64]int
	Ignored                 map[int64]int
	RunAt                   map[int64]string
	Runs                    []store.DriftRun
	Findings                []store.DriftFinding
	Ignores                 []store.IgnoreRule
	InstancesWithDrift      []instanceWithDrift
	ClusterGroups           []clusterGroup
	TotalDivergentObjects   int
	ClustersWithoutBaseline int
	Empty                   bool
}

func (s *Server) drift(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := driftData{Baselines: store.BaselineLabels, Counts: map[int64]int{},
		Ignored: map[int64]int{}, RunAt: map[int64]string{}}
	d.ScopeFilter = r.URL.Query().Get("scope")
	d.Baseline = r.URL.Query().Get("baseline")

	var err error
	if d.Clusters, err = s.DB.Clusters(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	scope := r.URL.Query().Get("cluster")
	d.ClusterID, _ = strconv.ParseInt(scope, 10, 64)
	d.AllScope = scope == "all" || (scope == "" && len(d.Clusters) == 0)
	if d.ClusterID == 0 && !d.AllScope && len(d.Clusters) > 0 {
		d.ClusterID = d.Clusters[0].ID
	}
	instances, _ := s.DB.Instances(ctx)
	if scope == "" && !d.AllScope {
		if runs, err := s.DB.DriftRuns(ctx, 0, d.Baseline); err == nil {
			member := map[int64]int64{}
			for _, in := range instances {
				if in.ClusterID.Valid {
					member[in.ID] = in.ClusterID.Int64
				}
			}
			var here, anywhere bool
			for _, run := range runs {
				if run.FindingCount == 0 {
					continue
				}
				anywhere = true
				if member[run.InstanceID] == d.ClusterID {
					here = true
				}
			}
			d.AllScope = anywhere && !here
		}
	}
	if d.AllScope {
		d.ClusterID, d.Scope = 0, "all"
	} else {
		d.Scope = strconv.FormatInt(d.ClusterID, 10)
	}
	for _, c := range d.Clusters {
		if c.ID == d.ClusterID {
			d.Cluster = c
		}
	}

	for _, in := range instances {
		switch {
		case d.AllScope:
			d.Members = append(d.Members, in)
		case in.ClusterID.Valid && in.ClusterID.Int64 == d.ClusterID:
			d.Members = append(d.Members, in)
		}
	}
	d.Empty = len(instances) == 0

	if d.ClusterID > 0 {
		d.Ignores, _ = s.DB.IgnoreRules(ctx, d.ClusterID)
	}
	d.Runs, _ = s.DB.DriftRuns(ctx, d.ClusterID, d.Baseline)
	var runIDs []int64
	for _, run := range d.Runs {
		runIDs = append(runIDs, run.ID)
		d.Counts[run.InstanceID] = run.FindingCount
		d.Ignored[run.InstanceID] = run.IgnoredCount
		d.RunAt[run.ID] = run.ComputedAt
	}
	d.Findings, _ = s.DB.DriftFindings(ctx, runIDs)

	// Build the new work queue structure
	d.InstancesWithDrift, d.ClusterGroups = s.buildDriftWorkQueue(ctx, d.Members, d.Findings, d.ScopeFilter, d.Clusters)
	d.TotalDivergentObjects = countDistinctObjects(d.Findings)
	d.ClustersWithoutBaseline = countClustersWithoutBaseline(d.Clusters)

	s.render(w, r, "drift.html", "Drift", d)
}

// groupFindings collapses one finding per instance into one row per object, which
// is how the question is actually asked: "what is different about this location",
// not "what does each host say".
func groupFindings(list []store.DriftFinding, by string) []driftGroup {
	order := []string{}
	groups := map[string]*driftGroup{}
	for _, f := range list {
		key := f.ObjectKind + " " + f.NaturalKey
		if by == "instance" {
			key = f.InstanceName
		}
		g := groups[key]
		if g == nil {
			g = &driftGroup{Kind: f.ObjectKind, Key: f.NaturalKey, Change: f.Change,
				FirstSeen: f.Field}
			if by == "instance" {
				g.Kind, g.Key = "instance", f.InstanceName
			}
			groups[key] = g
			order = append(order, key)
		}
		g.Findings = append(g.Findings, f)
		if !slicesHas(g.Instances, f.InstanceName) {
			g.Instances = append(g.Instances, f.InstanceName)
		}
	}
	out := make([]driftGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *groups[k])
	}
	// Most-diverged first: the object that differs on the most instances is the
	// one worth reading first.
	sort.SliceStable(out, func(i, j int) bool {
		return len(out[i].Instances) > len(out[j].Instances)
	})
	return out
}

func slicesHas(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// driftRecompute re-diffs every member of a cluster. It reports what it skipped:
// an instance whose snapshot predates the current parser version is refused
// rather than compared, because comparing across versions invents drift.
func (s *Server) driftRecompute(w http.ResponseWriter, r *http.Request) {
	scope := r.FormValue("cluster")
	id, _ := strconv.ParseInt(scope, 10, 64)
	// "all" is the every-instance scope, which the store has always supported as
	// cluster 0. Only a missing scope is an error now.
	all := scope == "all"
	if id == 0 && !all {
		redirect(w, r, "/drift", "", "pick a scope to re-diff")
		return
	}
	u := userOf(r)
	s.DB.Audit(r.Context(), &u.ID, "drift.recompute", "cluster", &id, r.FormValue("baseline"))
	stored, skipped := s.DB.RecomputeCluster(r.Context(), id)
	back := fmt.Sprintf("/drift?cluster=%d", id)
	if all {
		back = "/drift?cluster=all"
	}
	if len(skipped) > 0 {
		redirect(w, r, back, "", fmt.Sprintf("%d compared; %d skipped — %s",
			stored, len(skipped), strings.Join(skipped, "; ")))
		return
	}
	redirect(w, r, back, fmt.Sprintf("%d instance(s) compared", stored), "")
}

func (s *Server) driftIgnore(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	back := fmt.Sprintf("/drift?cluster=%d", id)
	rule := store.IgnoreRule{
		ClusterID:  id,
		ObjectKind: strings.TrimSpace(r.FormValue("object_kind")),
		Field:      strings.TrimSpace(r.FormValue("field")),
		Pattern:    strings.TrimSpace(r.FormValue("pattern")),
		Reason:     strings.TrimSpace(r.FormValue("reason")),
	}
	if _, err := s.DB.AddIgnoreRule(ctx, rule, &u.ID); err != nil {
		redirect(w, r, back, "", err.Error())
		return
	}
	redirect(w, r, back, "ignored; matching findings stay visible as ignored", "")
}

func (s *Server) driftUnignore(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	if err := s.DB.DeleteIgnoreRule(r.Context(), idOf(r, "id"), &u.ID); err != nil {
		redirect(w, r, "/drift", "", err.Error())
		return
	}
	redirect(w, r, "/drift?cluster="+r.FormValue("cluster"), "ignore rule removed", "")
}

// driftGolden declares a golden peer. Declared, not detected: nagipath has no
// opinion which side of a divergence is correct, so a human names the reference.
func (s *Server) driftGolden(w http.ResponseWriter, r *http.Request) {
	u := userOf(r)
	clusterID, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	var inst *int64
	if v, err := strconv.ParseInt(r.FormValue("instance"), 10, 64); err == nil && v > 0 {
		inst = &v
	}
	back := fmt.Sprintf("/drift?cluster=%d", clusterID)
	if err := s.DB.SetGoldenPeer(r.Context(), clusterID, inst, &u.ID); err != nil {
		redirect(w, r, back, "", err.Error())
		return
	}
	redirect(w, r, back, "golden peer set", "")
}

// buildDriftWorkQueue constructs the work queue structure: cluster-grouped rows
// of instances with divergences, with object breakdowns and first-seen times.
func (s *Server) buildDriftWorkQueue(ctx context.Context, members []store.Instance, findings []store.DriftFinding, scopeFilter string, clusters []store.Cluster) ([]instanceWithDrift, []clusterGroup) {
	// Filter findings by object kind if requested
	filtered := findings
	if scopeFilter != "" {
		filtered = nil
		for _, f := range findings {
			if f.ObjectKind == scopeFilter {
				filtered = append(filtered, f)
			}
		}
	}

	// Count findings per instance and track object kinds
	type instanceStats struct {
		id          int64
		count       int
		kinds       map[string]int
		firstSeen   string
		clusterID   int64
		displayName string
		nodeDisplay string
	}
	stats := map[int64]*instanceStats{}

	for _, f := range filtered {
		if f.IgnoredBy.Valid {
			continue
		}
		s := stats[f.InstanceID]
		if s == nil {
			s = &instanceStats{id: f.InstanceID, kinds: map[string]int{}}
			stats[f.InstanceID] = s
		}
		s.count++
		s.kinds[f.ObjectKind]++
		if s.firstSeen == "" || f.Field < s.firstSeen {
			s.firstSeen = f.Field
		}
	}

	// Fill in instance details
	for _, m := range members {
		if s := stats[m.ID]; s != nil {
			s.displayName = m.DisplayName
			s.nodeDisplay = m.NodeDisplayName
			if m.ClusterID.Valid {
				s.clusterID = m.ClusterID.Int64
			}
		}
	}

	// Group by cluster
	clusterMap := map[int64]*clusterGroup{}
	noCluster := &clusterGroup{ClusterName: "No cluster"}

	for _, m := range members {
		cid := int64(0)
		if m.ClusterID.Valid {
			cid = m.ClusterID.Int64
		}
		g := clusterMap[cid]
		if g == nil {
			g = &clusterGroup{}
			if cid > 0 {
				for _, c := range clusters {
					if c.ID == cid {
						g.ClusterName = c.Name
						if c.GoldenPeerName != "" {
							g.BaselineName = c.GoldenPeerName
						}
						break
					}
				}
			} else {
				g = noCluster
			}
			clusterMap[cid] = g
		}
		g.TotalNodes++
		if s := stats[m.ID]; s != nil && s.count > 0 {
			g.NodesWithDrift++
			// Build object breakdown
			breakdown := []string{}
			for _, kind := range []string{"route", "upstream", "site", "listener", "rule", "upstream_member"} {
				if n := s.kinds[kind]; n > 0 {
					plural := kind + "s"
					if n == 1 {
						plural = kind
					}
					breakdown = append(breakdown, fmt.Sprintf("%d %s", n, plural))
				}
			}
			g.Instances = append(g.Instances, instanceWithDrift{
				ID:              s.id,
				DisplayName:     s.displayName,
				NodeDisplayName: s.nodeDisplay,
				DivergenceCount: s.count,
				ObjectBreakdown: strings.Join(breakdown, " · "),
				FirstSeen:       s.firstSeen,
			})
		}
	}

	// Flatten to lists
	var allInstances []instanceWithDrift
	var groups []clusterGroup
	for _, cid := range sortedClusterIDs(clusterMap) {
		g := clusterMap[cid]
		if len(g.Instances) > 0 {
			groups = append(groups, *g)
			allInstances = append(allInstances, g.Instances...)
		}
	}

	return allInstances, groups
}

func sortedClusterIDs(m map[int64]*clusterGroup) []int64 {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		// 0 (no cluster) goes last
		if ids[i] == 0 {
			return false
		}
		if ids[j] == 0 {
			return true
		}
		return ids[i] < ids[j]
	})
	return ids
}

func countDistinctObjects(findings []store.DriftFinding) int {
	seen := map[string]bool{}
	for _, f := range findings {
		if !f.IgnoredBy.Valid {
			key := f.ObjectKind + "\x00" + f.NaturalKey
			seen[key] = true
		}
	}
	return len(seen)
}

func countClustersWithoutBaseline(clusters []store.Cluster) int {
	n := 0
	for _, c := range clusters {
		if !c.GoldenPeer.Valid {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- drift review

type driftReviewData struct {
	Instance        store.Instance
	Node            store.Node
	ObjectCount     int
	BaselineLabel   string
	SubjectTime     string
	BaselineTime    string
	ObjectGroups    []driftObjectGroup
	NextInstanceID  int64
	NextInstanceURL string
	ConformingCount int
	IgnoredCount    int
}

type driftObjectGroup struct {
	Kind     string
	Findings []store.DriftFinding
}

func (s *Server) driftReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := idOf(r, "instanceID")

	inst, err := s.DB.Instance(ctx, instanceID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	node, _ := s.DB.Node(ctx, inst.NodeID)

	// Get the most recent drift run for this instance
	runs, _ := s.DB.DriftRuns(ctx, 0, "")
	var run *store.DriftRun
	for i := range runs {
		if runs[i].InstanceID == instanceID {
			run = &runs[i]
			break
		}
	}
	if run == nil {
		redirect(w, r, "/drift", "", "no drift run found for this instance")
		return
	}

	findings, _ := s.DB.DriftFindings(ctx, []int64{run.ID})

	// Group findings by object kind
	kindMap := map[string][]store.DriftFinding{}
	conforming := 0
	ignored := 0
	for _, f := range findings {
		if f.IgnoredBy.Valid {
			ignored++
		} else {
			kindMap[f.ObjectKind] = append(kindMap[f.ObjectKind], f)
		}
	}

	// Count conforming objects (objects that exist but have no findings)
	// This is a rough estimate - actual count would require querying all objects
	conforming = 0

	var groups []driftObjectGroup
	for _, kind := range []string{"route", "upstream", "site", "listener", "rule", "upstream_member"} {
		if list := kindMap[kind]; len(list) > 0 {
			groups = append(groups, driftObjectGroup{Kind: kind, Findings: list})
		}
	}

	// Find next divergent instance in the same cluster
	nextID := int64(0)
	if inst.ClusterID.Valid {
		members, _ := s.DB.ClusterMembers(ctx, inst.ClusterID.Int64)
		foundCurrent := false
		for _, m := range members {
			if foundCurrent && m.Divergence.Valid && m.Divergence.Int64 > 0 {
				nextID = m.ID
				break
			}
			if m.ID == instanceID {
				foundCurrent = true
			}
		}
	}

	d := driftReviewData{
		Instance:        inst,
		Node:            node,
		ObjectCount:     len(findings) - ignored,
		BaselineLabel:   run.BaselineLabel,
		SubjectTime:     run.ComputedAt,
		BaselineTime:    run.ComputedAt,
		ObjectGroups:    groups,
		NextInstanceID:  nextID,
		ConformingCount: conforming,
		IgnoredCount:    ignored,
	}
	if nextID > 0 {
		d.NextInstanceURL = fmt.Sprintf("/drift/review/%d", nextID)
	}

	s.render(w, r, "drift_review.html", inst.DisplayName, d)
}
