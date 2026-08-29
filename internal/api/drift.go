package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// instanceWithDrift and clusterGroup are getDrift's working accumulator
// types, ported unchanged from internal/web/drift.go's buildDriftWorkQueue.
type instanceWithDrift struct {
	ID              int64
	DisplayName     string
	NodeDisplayName string
	DivergenceCount int
	ObjectBreakdown string
	FirstSeen       string
}

type clusterGroup struct {
	ClusterName    string
	BaselineName   string
	NodesWithDrift int
	TotalNodes     int
	Instances      []instanceWithDrift
}

// getDrift ports internal/web/drift.go's drift(): same scope resolution
// (?cluster=, defaulting to wherever drift actually is), same ?baseline=
// and ?scope= (object-kind) filters, same work-queue grouping.
func (s *Server) getDrift(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	resp := &pb.DriftResponse{}
	for _, b := range store.BaselineLabels {
		resp.Baselines = append(resp.Baselines, &pb.DriftBaselineOption{Kind: b.Kind, Label: b.Label})
	}
	resp.ScopeFilter = q.Get("scope")
	resp.Baseline = q.Get("baseline")

	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	for _, c := range clusters {
		resp.Clusters = append(resp.Clusters, &pb.DriftClusterOption{Id: c.ID, Name: c.Name, Members: int32(c.Members)})
	}

	scope := q.Get("cluster")
	clusterID, _ := strconv.ParseInt(scope, 10, 64)
	allScope := scope == "all" || (scope == "" && len(clusters) == 0)
	if clusterID == 0 && !allScope && len(clusters) > 0 {
		clusterID = clusters[0].ID
	}
	instances, _ := s.DB.Instances(ctx)
	if scope == "" && !allScope {
		if runs, err := s.DB.DriftRuns(ctx, 0, resp.Baseline); err == nil {
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
				if member[run.InstanceID] == clusterID {
					here = true
				}
			}
			allScope = anywhere && !here
		}
	}
	if allScope {
		clusterID, resp.Scope = 0, "all"
	} else {
		resp.Scope = strconv.FormatInt(clusterID, 10)
	}
	resp.ClusterId = clusterID
	resp.AllScope = allScope
	for _, c := range clusters {
		if c.ID == clusterID {
			resp.ClusterName = c.Name
		}
	}

	var members []store.Instance
	for _, in := range instances {
		if allScope || (in.ClusterID.Valid && in.ClusterID.Int64 == clusterID) {
			members = append(members, in)
		}
	}
	resp.MemberCount = int32(len(members))
	resp.Empty = len(instances) == 0

	if clusterID > 0 {
		ignores, _ := s.DB.IgnoreRules(ctx, clusterID)
		for _, ig := range ignores {
			resp.Ignores = append(resp.Ignores, &pb.IgnoreRule{
				Id: ig.ID, ClusterId: ig.ClusterID, ObjectKind: ig.ObjectKind, Field: ig.Field,
				Pattern: ig.Pattern, Reason: ig.Reason, CreatedAt: ig.CreatedAt, Matched: int32(ig.Matched),
			})
		}
	}
	runs, _ := s.DB.DriftRuns(ctx, clusterID, resp.Baseline)
	var runIDs []int64
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
		resp.Runs = append(resp.Runs, &pb.DriftRun{
			Id: run.ID, InstanceId: run.InstanceID, BaselineKind: run.BaselineKind,
			BaselineLabel: run.BaselineLabel, ComputedAt: run.ComputedAt, FindingCount: int32(run.FindingCount),
			InstanceName: run.InstanceName, NodeName: run.NodeName, Vendor: run.Vendor,
			ParserVersion: int32(run.ParserVersion), IgnoredCount: int32(run.IgnoredCount), IsGoldenPeer: run.IsGoldenPeer,
		})
	}
	findings, _ := s.DB.DriftFindings(ctx, runIDs)

	instancesWithDrift, groups := buildDriftWorkQueue(members, findings, resp.ScopeFilter, clusters)
	for _, iw := range instancesWithDrift {
		resp.InstancesWithDrift = append(resp.InstancesWithDrift, &pb.DriftInstanceRow{
			Id: iw.ID, DisplayName: iw.DisplayName, NodeDisplayName: iw.NodeDisplayName,
			DivergenceCount: int32(iw.DivergenceCount), ObjectBreakdown: iw.ObjectBreakdown, FirstSeen: iw.FirstSeen,
		})
	}
	for _, g := range groups {
		cg := &pb.DriftClusterGroup{
			ClusterName: g.ClusterName, BaselineName: g.BaselineName,
			NodesWithDrift: int32(g.NodesWithDrift), TotalNodes: int32(g.TotalNodes),
		}
		for _, iw := range g.Instances {
			cg.Instances = append(cg.Instances, &pb.DriftInstanceRow{
				Id: iw.ID, DisplayName: iw.DisplayName, NodeDisplayName: iw.NodeDisplayName,
				DivergenceCount: int32(iw.DivergenceCount), ObjectBreakdown: iw.ObjectBreakdown, FirstSeen: iw.FirstSeen,
			})
		}
		resp.ClusterGroups = append(resp.ClusterGroups, cg)
	}
	resp.TotalDivergentObjects = int32(countDistinctObjects(findings))
	resp.ClustersWithoutBaseline = int32(countClustersWithoutBaseline(clusters))

	writeProto(w, http.StatusOK, resp)
}

// buildDriftWorkQueue is internal/web/drift.go's method of the same name,
// unchanged, minus the ctx and *Server parameters it never used.
func buildDriftWorkQueue(members []store.Instance, findings []store.DriftFinding, scopeFilter string, clusters []store.Cluster) ([]instanceWithDrift, []clusterGroup) {
	filtered := findings
	if scopeFilter != "" {
		filtered = nil
		for _, f := range findings {
			if f.ObjectKind == scopeFilter {
				filtered = append(filtered, f)
			}
		}
	}

	type instanceStats struct {
		id          int64
		count       int
		kinds       map[string]int
		firstSeen   string
		displayName string
		nodeDisplay string
	}
	stats := map[int64]*instanceStats{}

	for _, f := range filtered {
		if f.IgnoredBy.Valid {
			continue
		}
		st := stats[f.InstanceID]
		if st == nil {
			st = &instanceStats{id: f.InstanceID, kinds: map[string]int{}}
			stats[f.InstanceID] = st
		}
		st.count++
		st.kinds[f.ObjectKind]++
		if st.firstSeen == "" || f.Field < st.firstSeen {
			st.firstSeen = f.Field
		}
	}

	for _, m := range members {
		if st := stats[m.ID]; st != nil {
			st.displayName = m.DisplayName
			st.nodeDisplay = m.NodeDisplayName
		}
	}

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
		if st := stats[m.ID]; st != nil && st.count > 0 {
			g.NodesWithDrift++
			var breakdown []string
			for _, kind := range []string{"route", "upstream", "site", "listener", "rule", "upstream_member"} {
				if n := st.kinds[kind]; n > 0 {
					plural := kind + "s"
					if n == 1 {
						plural = kind
					}
					breakdown = append(breakdown, fmt.Sprintf("%d %s", n, plural))
				}
			}
			g.Instances = append(g.Instances, instanceWithDrift{
				ID: st.id, DisplayName: st.displayName, NodeDisplayName: st.nodeDisplay,
				DivergenceCount: st.count, ObjectBreakdown: strings.Join(breakdown, " · "), FirstSeen: st.firstSeen,
			})
		}
	}

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
			seen[f.ObjectKind+"\x00"+f.NaturalKey] = true
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

// getDriftReview ports internal/web/drift.go's driftReview().
func (s *Server) getDriftReview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID, _ := strconv.ParseInt(r.PathValue("instanceID"), 10, 64)

	inst, err := s.DB.Instance(ctx, instanceID)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "No such instance.")
		return
	}
	node, _ := s.DB.Node(ctx, inst.NodeID)

	runs, _ := s.DB.DriftRuns(ctx, 0, "")
	var run *store.DriftRun
	for i := range runs {
		if runs[i].InstanceID == instanceID {
			run = &runs[i]
			break
		}
	}
	if run == nil {
		apiError(w, http.StatusNotFound, "no_drift_run", "No drift run found for this instance.")
		return
	}

	findings, _ := s.DB.DriftFindings(ctx, []int64{run.ID})
	kindMap := map[string][]store.DriftFinding{}
	ignored := 0
	for _, f := range findings {
		if f.IgnoredBy.Valid {
			ignored++
		} else {
			kindMap[f.ObjectKind] = append(kindMap[f.ObjectKind], f)
		}
	}

	resp := &pb.DriftReviewResponse{
		InstanceId: instanceID, InstanceDisplayName: inst.DisplayName, NodeDisplayName: node.DisplayName,
		NodeId: node.ID, ObjectCount: int32(len(findings) - ignored), BaselineLabel: run.BaselineLabel,
		SubjectTime: run.ComputedAt, BaselineTime: run.ComputedAt, IgnoredCount: int32(ignored),
	}
	for _, kind := range []string{"route", "upstream", "site", "listener", "rule", "upstream_member"} {
		list := kindMap[kind]
		if len(list) == 0 {
			continue
		}
		g := &pb.DriftObjectGroup{Kind: kind}
		for _, f := range list {
			g.Findings = append(g.Findings, &pb.DriftFinding{
				Id: f.ID, ObjectKind: f.ObjectKind, NaturalKey: f.NaturalKey, Change: f.Change,
				Field: f.Field, BaselineText: f.BaselineText, SubjectText: f.SubjectText, ActionClass: f.ActionClass,
				Provenance: toPBProvenance(f.Path, f.FileID, f.SnapshotID, f.ByteStart),
			})
		}
		resp.ObjectGroups = append(resp.ObjectGroups, g)
	}

	if inst.ClusterID.Valid {
		members, _ := s.DB.ClusterMembers(ctx, inst.ClusterID.Int64)
		foundCurrent := false
		for _, m := range members {
			if foundCurrent && m.Divergence.Valid && m.Divergence.Int64 > 0 {
				resp.NextInstanceId = m.ID
				break
			}
			if m.ID == instanceID {
				foundCurrent = true
			}
		}
	}

	writeProto(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------- mutations

func (s *Server) postDriftRecompute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cluster  string `json:"cluster"`
		Baseline string `json:"baseline"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	all := body.Cluster == "all"
	id, _ := strconv.ParseInt(body.Cluster, 10, 64)
	if id == 0 && !all {
		apiError(w, http.StatusUnprocessableEntity, "scope_required", "Pick a scope to re-diff.")
		return
	}
	u := userOf(r)
	s.DB.Audit(r.Context(), &u.ID, "drift.recompute", "cluster", &id, body.Baseline)
	stored, skipped := s.DB.RecomputeCluster(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "compared": stored, "skipped": skipped})
}

func (s *Server) postDriftIgnore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cluster    int64  `json:"cluster"`
		ObjectKind string `json:"object_kind"`
		Field      string `json:"field"`
		Pattern    string `json:"pattern"`
		Reason     string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	u := userOf(r)
	rule := store.IgnoreRule{
		ClusterID: body.Cluster, ObjectKind: strings.TrimSpace(body.ObjectKind),
		Field: strings.TrimSpace(body.Field), Pattern: strings.TrimSpace(body.Pattern),
		Reason: strings.TrimSpace(body.Reason),
	}
	if _, err := s.DB.AddIgnoreRule(r.Context(), rule, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "ignore_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) postDriftUnignore(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u := userOf(r)
	if err := s.DB.DeleteIgnoreRule(r.Context(), id, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "unignore_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// postDriftGolden declares a golden peer. Declared, not detected: nagipath
// has no opinion which side of a divergence is correct, so a human names
// the reference. Used by both /drift and ClustersPage's "Change baseline".
func (s *Server) postDriftGolden(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cluster  int64 `json:"cluster"`
		Instance int64 `json:"instance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	var inst *int64
	if body.Instance > 0 {
		inst = &body.Instance
	}
	u := userOf(r)
	if err := s.DB.SetGoldenPeer(r.Context(), body.Cluster, inst, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "golden_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
