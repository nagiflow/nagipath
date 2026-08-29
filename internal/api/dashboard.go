package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// dashboardAttention is one row of "needs attention". Tone is the badge
// color class the frontend renders, computed here (attentionTone) so the
// severity filter below and the badge can never disagree — the same
// constraint internal/web/dashboard.go's comment records for the pair.
type dashboardAttention struct {
	Kind    string `json:"kind"`
	Tone    string `json:"tone"`
	Text    string `json:"text"`
	Note    string `json:"note"`
	Link    string `json:"link"`
	Cluster string `json:"cluster,omitempty"`
	Since   string `json:"since,omitempty"`
}

type dashboardActivityBucket struct {
	Label    string `json:"label"`
	Total    int    `json:"total"`
	Failed   int    `json:"failed"`
	Degraded int    `json:"degraded"`
}

type dashboardRiskCluster struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Nodes        int    `json:"nodes"`
	Fresh        int    `json:"fresh"`
	FreshPercent int    `json:"fresh_percent"`
	Drift        int    `json:"drift"`
	CertsLabel   string `json:"certs_label"`
	State        string `json:"state"`
	Risk         int    `json:"risk"`
}

type dashboardResponse struct {
	Nodes            int    `json:"nodes"`
	Instances        int    `json:"instances"`
	VendorCount      int    `json:"vendor_count"`
	Degraded         int    `json:"degraded"`
	PendingHostKeys  int    `json:"pending_host_keys"`
	OKInstances      int    `json:"ok_instances"`
	Unparsed         int    `json:"unparsed_instances"`
	PendingInstances int    `json:"pending_instances"`
	Certs30          int    `json:"certs_expiring_30d"`
	Drifted          int    `json:"drifted_instances"`
	DriftedClusters  int    `json:"drifted_clusters"`
	FreshInstances   int    `json:"fresh_instances"`
	AgingInstances   int    `json:"aging_instances"`
	StaleInstances   int    `json:"stale_instances"`
	TotalRules       int    `json:"total_rules"`
	CertBindings     int    `json:"cert_bindings_expiring"`
	UnreachableNodes int    `json:"unreachable_nodes"`
	UnreachableSince string `json:"unreachable_since,omitempty"`

	Attention    []dashboardAttention      `json:"attention"`
	Activity     []dashboardActivityBucket `json:"activity"`
	ActivityMax  int                       `json:"activity_max"`
	RiskClusters []dashboardRiskCluster    `json:"risk_clusters"`
	RecentTraces []map[string]any          `json:"recent_traces"`
	Collections  []map[string]any          `json:"recent_collections"`
	Expiring     []map[string]any          `json:"expiring_certificates"`
	Clusters     []map[string]any          `json:"clusters"`

	SelectedCluster   int64  `json:"selected_cluster"`
	AttentionSeverity string `json:"attention_severity"`
	AttentionCluster  int64  `json:"attention_cluster"`

	OldestSnapshot        string  `json:"oldest_snapshot,omitempty"`
	OldestSnapshotNode    string  `json:"oldest_snapshot_node,omitempty"`
	AvgCollectionDuration float64 `json:"avg_collection_duration_seconds"`
}

// attentionTone matches internal/web/dashboard.go's attentionTone exactly —
// both the badge color and the severity filter (?severity=) depend on it.
func attentionTone(kind string) string {
	switch kind {
	case "QUARANTINED":
		return "err"
	case "DEGRADED", "CERT", "STALE", "DRIFT":
		return "deg"
	}
	return "inf"
}

// getDashboard is the fleet-overview screen: counts, the needs-attention
// list, the 24h activity strip and a per-cluster risk ranking. Ported field
// for field from internal/web/dashboard.go's dashboard() — same queries,
// same filters (?cluster=, ?severity=, ?attention_cluster=), same tone/risk
// rules — just returned as JSON instead of rendered into dashboard.html.
func (s *Server) getDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	// List fields default to [] rather than Go's zero-value nil slice — a JS
	// consumer calling .map() on a nil-turned-null field is a crash, not a
	// no-op, and an empty fleet is the state every install starts in.
	resp := dashboardResponse{
		Attention: []dashboardAttention{}, RiskClusters: []dashboardRiskCluster{},
		RecentTraces: []map[string]any{}, Collections: []map[string]any{},
		Expiring: []map[string]any{}, Clusters: []map[string]any{},
	}
	if cid := q.Get("cluster"); cid != "" {
		if id, err := strconv.ParseInt(cid, 10, 64); err == nil {
			resp.SelectedCluster = id
		}
	}
	resp.AttentionSeverity = q.Get("severity")
	if resp.AttentionSeverity == "" {
		resp.AttentionSeverity = "all"
	}
	if acid := q.Get("attention_cluster"); acid != "" {
		if id, err := strconv.ParseInt(acid, 10, 64); err == nil {
			resp.AttentionCluster = id
		}
	}

	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	for _, cl := range clusters {
		resp.Clusters = append(resp.Clusters, map[string]any{"id": cl.ID, "name": cl.Name})
	}

	allNodes, err := s.DB.Nodes(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	allInstances, err := s.DB.Instances(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	var nodes []store.Node
	var instances []store.Instance
	if resp.SelectedCluster == 0 {
		nodes, instances = allNodes, allInstances
	} else {
		inCluster := map[int64]bool{}
		for _, in := range allInstances {
			if in.ClusterID.Valid && in.ClusterID.Int64 == resp.SelectedCluster {
				instances = append(instances, in)
				inCluster[in.NodeID] = true
			}
		}
		for _, n := range allNodes {
			if inCluster[n.ID] {
				nodes = append(nodes, n)
			}
		}
	}

	quarantineThreshold := s.DB.SettingInt(ctx, "quarantine_after_failures")
	now := time.Now().UTC()
	sixHoursAgo := now.Add(-6 * time.Hour).Format("2006-01-02T15:04:05Z")
	oneDayAgo := now.Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z")

	var attention []dashboardAttention
	var earliestUnreachable string
	for _, n := range nodes {
		if n.ConsecutiveFailures > 0 {
			resp.UnreachableNodes++
			if n.LastCollection.Valid && (earliestUnreachable == "" || n.LastCollection.String < earliestUnreachable) {
				earliestUnreachable = n.LastCollection.String
			}
			kind := "DEGRADED"
			if store.Quarantined(n.ConsecutiveFailures, quarantineThreshold) {
				kind = "QUARANTINED"
			}
			attention = append(attention, dashboardAttention{
				Kind: kind, Tone: attentionTone(kind), Text: n.DisplayName,
				Note: fmt.Sprintf("%d consecutive failures", n.ConsecutiveFailures),
				Link: fmt.Sprintf("/nodes/%d", n.ID), Since: n.LastCollection.String,
			})
		}
	}
	resp.UnreachableSince = earliestUnreachable
	resp.Nodes = len(nodes)
	resp.Instances = len(instances)

	vendors := map[string]int{}
	for _, in := range instances {
		vendors[in.Vendor]++
		if in.LastCaptured.Valid {
			switch {
			case in.LastCaptured.String >= sixHoursAgo:
				resp.FreshInstances++
			case in.LastCaptured.String >= oneDayAgo:
				resp.AgingInstances++
			default:
				resp.StaleInstances++
			}
		}
		subject := in.DisplayName
		if in.NodeDisplayName != "" {
			subject = in.NodeDisplayName + " · " + in.DisplayName
		}
		switch in.State() {
		case "ok":
			resp.OKInstances++
		case "pending":
			resp.PendingInstances++
		case "unparsed":
			resp.Unparsed++
			attention = append(attention, dashboardAttention{Kind: "DEGRADED", Tone: attentionTone("DEGRADED"),
				Text: subject, Note: "parse " + in.ParseState, Link: fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName, Since: in.LastCaptured.String})
		case "degraded":
			resp.Degraded++
			attention = append(attention, dashboardAttention{Kind: "DEGRADED", Tone: attentionTone("DEGRADED"),
				Text: subject, Note: "incomplete snapshot", Link: fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName, Since: in.LastCaptured.String})
		}
		if len(in.AccessLogPaths) == 0 && in.State() != "pending" {
			attention = append(attention, dashboardAttention{Kind: "LOG FORMAT", Tone: attentionTone("LOG FORMAT"),
				Text: subject, Note: "no access log path derivable", Link: fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName, Since: in.LastCaptured.String})
		}
	}
	resp.VendorCount = len(vendors)

	if staleCount, _ := s.DB.StaleNodeCount(ctx, oneDayAgo); staleCount > 0 {
		attention = append(attention, dashboardAttention{Kind: "STALE", Tone: attentionTone("STALE"),
			Text: fmt.Sprintf("%d nodes not collected in 24h", staleCount), Link: "/nodes", Since: oneDayAgo})
	}

	if ruleCount, err := s.DB.RuleCount(ctx); err == nil {
		resp.TotalRules = ruleCount
	}
	resp.Degraded += resp.Unparsed
	resp.PendingHostKeys = s.DB.PendingHostKeyCount(ctx)
	if resp.PendingHostKeys > 0 {
		attention = append(attention, dashboardAttention{Kind: "HOST KEY", Tone: attentionTone("HOST KEY"),
			Text: fmt.Sprintf("%d host key(s) waiting", resp.PendingHostKeys), Link: "/settings/hostkeys"})
	}

	collections, _ := s.DB.Collections(ctx, 200)
	resp.Activity, resp.ActivityMax = dashboardActivity(now, collections)
	if len(collections) > 5 {
		collections = collections[:5]
	}
	for _, c := range collections {
		resp.Collections = append(resp.Collections, map[string]any{
			"id": c.ID, "node_id": c.NodeID, "node_name": c.NodeName, "trigger": c.Trigger,
			"started_at": c.StartedAt, "status": c.Status, "error": c.Error,
		})
	}

	resp.AvgCollectionDuration, _ = s.DB.AvgCollectionDuration(ctx, oneDayAgo)
	resp.OldestSnapshot, resp.OldestSnapshotNode, _ = s.DB.OldestSnapshot(ctx)

	if runs, err := s.DB.DriftRuns(ctx, 0, ""); err == nil {
		clusterOf := map[int64]string{}
		clusterIDOf := map[int64]int64{}
		for _, in := range instances {
			clusterOf[in.ID] = in.ClusterName
			if in.ClusterID.Valid {
				clusterIDOf[in.ID] = in.ClusterID.Int64
			}
		}
		seen := map[int64]bool{}
		withDrift := map[int64]bool{}
		for _, run := range runs {
			if run.FindingCount > 0 && !seen[run.InstanceID] {
				seen[run.InstanceID] = true
				resp.Drifted++
				if cid, ok := clusterIDOf[run.InstanceID]; ok {
					withDrift[cid] = true
				}
				attention = append(attention, dashboardAttention{Kind: "DRIFT", Tone: attentionTone("DRIFT"),
					Text: run.NodeName + " · " + run.InstanceName,
					Note: fmt.Sprintf("%d divergence(s)", run.FindingCount), Link: "/drift",
					Cluster: clusterOf[run.InstanceID], Since: run.ComputedAt})
			}
		}
		resp.DriftedClusters = len(withDrift)
	}

	certs, _ := s.DB.Certificates(ctx)
	cutoff := now.AddDate(0, 0, 30).Format("2006-01-02T15:04:05Z")
	for _, c := range certs {
		if c.NotAfter != "" && c.NotAfter <= cutoff {
			resp.Certs30++
			resp.CertBindings += c.Bindings
			cn := c.SubjectCN
			if strings.TrimSpace(cn) == "" {
				cn = "(no CN)"
			}
			resp.Expiring = append(resp.Expiring, map[string]any{
				"id": c.ID, "subject_cn": c.SubjectCN, "not_after": c.NotAfter, "bindings": c.Bindings,
			})
			attention = append(attention, dashboardAttention{Kind: "CERT", Tone: attentionTone("CERT"),
				Text: cn, Note: fmt.Sprintf("%d binding(s)", c.Bindings),
				Link: fmt.Sprintf("/certificates?cert=%d", c.ID), Since: c.NotAfter})
		}
	}

	for _, a := range attention {
		if resp.AttentionSeverity != "all" && a.Tone != resp.AttentionSeverity {
			continue
		}
		if resp.AttentionCluster != 0 {
			var clusterName string
			for _, cl := range clusters {
				if cl.ID == resp.AttentionCluster {
					clusterName = cl.Name
					break
				}
			}
			if a.Cluster != clusterName && clusterName != "" {
				continue
			}
		}
		resp.Attention = append(resp.Attention, a)
	}

	if recent, err := trace.Recent(ctx, s.DB, 5); err == nil {
		for _, t := range recent {
			resp.RecentTraces = append(resp.RecentTraces, map[string]any{
				"id": t.ID, "hostname": t.Hostname, "path": t.Path, "computed_at": t.ComputedAt,
				"hop_count": t.HopCount, "confidence": t.Confidence, "terminal_reason": t.TerminalReason,
			})
		}
	}

	if len(clusters) > 0 {
		aggs, _ := s.DB.ClusterAggregates(ctx)
		pendingKeys := s.DB.PendingHostKeyCount(ctx)
		risk := []dashboardRiskCluster{}
		for _, cl := range clusters {
			if cl.Members == 0 {
				continue
			}
			row := dashboardRiskCluster{ID: cl.ID, Name: cl.Name, Nodes: cl.Members, State: "ok"}
			for _, in := range allInstances {
				if in.ClusterID.Valid && in.ClusterID.Int64 == cl.ID &&
					in.LastCaptured.Valid && in.LastCaptured.String >= sixHoursAgo {
					row.Fresh++
				}
			}
			var certsExpiring int
			if agg, ok := aggs[cl.ID]; ok {
				row.Drift, certsExpiring = agg.DriftCount, agg.CertsExpiring30d
			}
			if row.Nodes > 0 {
				row.FreshPercent = (row.Fresh * 100) / row.Nodes
			}
			if certsExpiring > 0 {
				row.CertsLabel = fmt.Sprintf("%d exp", certsExpiring)
			} else {
				row.CertsLabel = "—"
			}
			switch {
			case row.Fresh < row.Nodes:
				row.State, row.Risk = "failing", 3
			case row.Drift > 0:
				row.State, row.Risk = "drift", 2
			case pendingKeys > 0:
				row.State, row.Risk = fmt.Sprintf("pending %d", pendingKeys), 1
			default:
				row.State, row.Risk = "ok", 0
			}
			risk = append(risk, row)
		}
		sort.Slice(risk, func(i, j int) bool {
			if risk[i].Risk != risk[j].Risk {
				return risk[i].Risk > risk[j].Risk
			}
			return risk[i].Name < risk[j].Name
		})
		if len(risk) > 5 {
			risk = risk[:5]
		}
		resp.RiskClusters = risk
	}

	writeJSON(w, http.StatusOK, resp)
}

// dashboardActivity is internal/web/dashboard.go's activity(), unchanged:
// 24 hourly buckets over the collections already read above.
//
// ponytail: fixed buckets computed in Go over rows already in memory. A
// GROUP BY would be the upgrade if the window ever becomes a control
// instead of the fixed 24h the dashboard shows.
func dashboardActivity(now time.Time, list []store.Collection) ([]dashboardActivityBucket, int) {
	const buckets, span = 24, time.Hour
	now = now.UTC().Truncate(span)
	oldest := now.Add(-(buckets - 1) * span)
	out := make([]dashboardActivityBucket, buckets)
	for i := range out {
		out[i].Label = oldest.Add(time.Duration(i) * span).Format("15:04")
	}
	max := 0
	for _, c := range list {
		t, err := time.Parse("2006-01-02T15:04:05Z", c.StartedAt)
		if err != nil || t.Before(oldest) {
			continue
		}
		i := int(t.Sub(oldest) / span)
		if i >= buckets {
			i = buckets - 1
		}
		out[i].Total++
		if c.Status == "failed" {
			out[i].Failed++
		} else if c.Status == "degraded" {
			out[i].Degraded++
		}
		if out[i].Total > max {
			max = out[i].Total
		}
	}
	return out, max
}
