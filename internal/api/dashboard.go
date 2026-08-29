package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// dashboardAttention, dashboardActivityBucket and dashboardRiskCluster are
// the working accumulator types getDashboard computes into — plain value
// types rather than the pointer-heavy generated pb messages, which are
// nicer to sort.Slice and append to. dashboardBuild.toProto() converts the
// finished accumulator to proto/nagipath/api/v1/dashboard.proto's
// DashboardResponse (docs/adr/0018) once, at the end.
type dashboardAttention struct {
	Kind    string
	Tone    string
	Text    string
	Note    string
	Link    string
	Cluster string
	Since   string
}

type dashboardActivityBucket struct {
	Label    string
	Total    int
	Failed   int
	Degraded int
}

type dashboardRiskCluster struct {
	ID           int64
	Name         string
	Nodes        int
	Fresh        int
	FreshPercent int
	Drift        int
	CertsLabel   string
	State        string
	Risk         int
}

type dashboardBuild struct {
	Nodes            int
	Instances        int
	VendorCount      int
	Degraded         int
	PendingHostKeys  int
	OKInstances      int
	Unparsed         int
	PendingInstances int
	Certs30          int
	Drifted          int
	DriftedClusters  int
	FreshInstances   int
	AgingInstances   int
	StaleInstances   int
	TotalRules       int
	CertBindings     int
	UnreachableNodes int
	UnreachableSince string

	Attention    []dashboardAttention
	Activity     []dashboardActivityBucket
	ActivityMax  int
	RiskClusters []dashboardRiskCluster
	RecentTraces []trace.Summary
	Collections  []store.Collection
	Expiring     []store.CertificateView
	Clusters     []store.Cluster

	SelectedCluster   int64
	AttentionSeverity string
	AttentionCluster  int64

	OldestSnapshot        string
	OldestSnapshotNode    string
	AvgCollectionDuration float64
}

func (d *dashboardBuild) toProto() *pb.DashboardResponse {
	resp := &pb.DashboardResponse{
		Nodes: int32(d.Nodes), Instances: int32(d.Instances), VendorCount: int32(d.VendorCount),
		Degraded: int32(d.Degraded), PendingHostKeys: int32(d.PendingHostKeys), OkInstances: int32(d.OKInstances),
		UnparsedInstances: int32(d.Unparsed), PendingInstances: int32(d.PendingInstances),
		CertsExpiring_30D: int32(d.Certs30), DriftedInstances: int32(d.Drifted), DriftedClusters: int32(d.DriftedClusters),
		FreshInstances: int32(d.FreshInstances), AgingInstances: int32(d.AgingInstances), StaleInstances: int32(d.StaleInstances),
		TotalRules: int32(d.TotalRules), CertBindingsExpiring: int32(d.CertBindings), UnreachableNodes: int32(d.UnreachableNodes),
		UnreachableSince: d.UnreachableSince, ActivityMax: int32(d.ActivityMax),
		SelectedCluster: d.SelectedCluster, AttentionSeverity: d.AttentionSeverity, AttentionCluster: d.AttentionCluster,
		OldestSnapshot: d.OldestSnapshot, OldestSnapshotNode: d.OldestSnapshotNode,
		AvgCollectionDurationSeconds: d.AvgCollectionDuration,
	}
	for _, a := range d.Attention {
		resp.Attention = append(resp.Attention, &pb.DashboardAttention{
			Kind: a.Kind, Tone: a.Tone, Text: a.Text, Note: a.Note, Link: a.Link, Cluster: a.Cluster, Since: a.Since,
		})
	}
	for _, b := range d.Activity {
		resp.Activity = append(resp.Activity, &pb.DashboardActivityBucket{
			Label: b.Label, Total: int32(b.Total), Failed: int32(b.Failed), Degraded: int32(b.Degraded),
		})
	}
	for _, rc := range d.RiskClusters {
		resp.RiskClusters = append(resp.RiskClusters, &pb.DashboardRiskCluster{
			Id: rc.ID, Name: rc.Name, Nodes: int32(rc.Nodes), Fresh: int32(rc.Fresh),
			FreshPercent: int32(rc.FreshPercent), Drift: int32(rc.Drift), CertsLabel: rc.CertsLabel,
			State: rc.State, Risk: int32(rc.Risk),
		})
	}
	for _, t := range d.RecentTraces {
		resp.RecentTraces = append(resp.RecentTraces, &pb.DashboardTrace{
			Id: t.ID, Hostname: t.Hostname, Path: t.Path, ComputedAt: t.ComputedAt,
			HopCount: int32(t.HopCount), Confidence: t.Confidence, TerminalReason: t.TerminalReason,
		})
	}
	for _, c := range d.Collections {
		resp.RecentCollections = append(resp.RecentCollections, &pb.DashboardCollection{
			Id: c.ID, NodeId: c.NodeID, NodeName: c.NodeName, Trigger: c.Trigger,
			StartedAt: c.StartedAt, Status: c.Status, Error: c.Error,
		})
	}
	for _, c := range d.Expiring {
		resp.ExpiringCertificates = append(resp.ExpiringCertificates, &pb.DashboardCertificate{
			Id: c.ID, SubjectCn: c.SubjectCN, NotAfter: c.NotAfter, Bindings: int32(c.Bindings),
		})
	}
	for _, cl := range d.Clusters {
		resp.Clusters = append(resp.Clusters, &pb.DashboardCluster{Id: cl.ID, Name: cl.Name})
	}
	return resp
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
// rules — just returned as protojson instead of rendered into dashboard.html.
func (s *Server) getDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	d := dashboardBuild{}
	if cid := q.Get("cluster"); cid != "" {
		if id, err := strconv.ParseInt(cid, 10, 64); err == nil {
			d.SelectedCluster = id
		}
	}
	d.AttentionSeverity = q.Get("severity")
	if d.AttentionSeverity == "" {
		d.AttentionSeverity = "all"
	}
	if acid := q.Get("attention_cluster"); acid != "" {
		if id, err := strconv.ParseInt(acid, 10, 64); err == nil {
			d.AttentionCluster = id
		}
	}

	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	d.Clusters = clusters

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
	if d.SelectedCluster == 0 {
		nodes, instances = allNodes, allInstances
	} else {
		inCluster := map[int64]bool{}
		for _, in := range allInstances {
			if in.ClusterID.Valid && in.ClusterID.Int64 == d.SelectedCluster {
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
			d.UnreachableNodes++
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
	d.UnreachableSince = earliestUnreachable
	d.Nodes = len(nodes)
	d.Instances = len(instances)

	vendors := map[string]int{}
	for _, in := range instances {
		vendors[in.Vendor]++
		if in.LastCaptured.Valid {
			switch {
			case in.LastCaptured.String >= sixHoursAgo:
				d.FreshInstances++
			case in.LastCaptured.String >= oneDayAgo:
				d.AgingInstances++
			default:
				d.StaleInstances++
			}
		}
		subject := in.DisplayName
		if in.NodeDisplayName != "" {
			subject = in.NodeDisplayName + " · " + in.DisplayName
		}
		switch in.State() {
		case "ok":
			d.OKInstances++
		case "pending":
			d.PendingInstances++
		case "unparsed":
			d.Unparsed++
			attention = append(attention, dashboardAttention{Kind: "DEGRADED", Tone: attentionTone("DEGRADED"),
				Text: subject, Note: "parse " + in.ParseState, Link: fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName, Since: in.LastCaptured.String})
		case "degraded":
			d.Degraded++
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
	d.VendorCount = len(vendors)

	if staleCount, _ := s.DB.StaleNodeCount(ctx, oneDayAgo); staleCount > 0 {
		attention = append(attention, dashboardAttention{Kind: "STALE", Tone: attentionTone("STALE"),
			Text: fmt.Sprintf("%d nodes not collected in 24h", staleCount), Link: "/nodes", Since: oneDayAgo})
	}

	if ruleCount, err := s.DB.RuleCount(ctx); err == nil {
		d.TotalRules = ruleCount
	}
	d.Degraded += d.Unparsed
	d.PendingHostKeys = s.DB.PendingHostKeyCount(ctx)
	if d.PendingHostKeys > 0 {
		attention = append(attention, dashboardAttention{Kind: "HOST KEY", Tone: attentionTone("HOST KEY"),
			Text: fmt.Sprintf("%d host key(s) waiting", d.PendingHostKeys), Link: "/settings/hostkeys"})
	}

	collections, _ := s.DB.Collections(ctx, 200)
	d.Activity, d.ActivityMax = dashboardActivity(now, collections)
	if len(collections) > 5 {
		collections = collections[:5]
	}
	d.Collections = collections

	d.AvgCollectionDuration, _ = s.DB.AvgCollectionDuration(ctx, oneDayAgo)
	d.OldestSnapshot, d.OldestSnapshotNode, _ = s.DB.OldestSnapshot(ctx)

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
				d.Drifted++
				if cid, ok := clusterIDOf[run.InstanceID]; ok {
					withDrift[cid] = true
				}
				attention = append(attention, dashboardAttention{Kind: "DRIFT", Tone: attentionTone("DRIFT"),
					Text: run.NodeName + " · " + run.InstanceName,
					Note: fmt.Sprintf("%d divergence(s)", run.FindingCount), Link: "/drift",
					Cluster: clusterOf[run.InstanceID], Since: run.ComputedAt})
			}
		}
		d.DriftedClusters = len(withDrift)
	}

	certs, _ := s.DB.Certificates(ctx)
	cutoff := now.AddDate(0, 0, 30).Format("2006-01-02T15:04:05Z")
	for _, c := range certs {
		if c.NotAfter != "" && c.NotAfter <= cutoff {
			d.Certs30++
			d.CertBindings += c.Bindings
			cn := c.SubjectCN
			if strings.TrimSpace(cn) == "" {
				cn = "(no CN)"
			}
			d.Expiring = append(d.Expiring, c)
			attention = append(attention, dashboardAttention{Kind: "CERT", Tone: attentionTone("CERT"),
				Text: cn, Note: fmt.Sprintf("%d binding(s)", c.Bindings),
				Link: fmt.Sprintf("/certificates?cert=%d", c.ID), Since: c.NotAfter})
		}
	}

	for _, a := range attention {
		if d.AttentionSeverity != "all" && a.Tone != d.AttentionSeverity {
			continue
		}
		if d.AttentionCluster != 0 {
			var clusterName string
			for _, cl := range clusters {
				if cl.ID == d.AttentionCluster {
					clusterName = cl.Name
					break
				}
			}
			if a.Cluster != clusterName && clusterName != "" {
				continue
			}
		}
		d.Attention = append(d.Attention, a)
	}

	if recent, err := trace.Recent(ctx, s.DB, 5); err == nil {
		d.RecentTraces = recent
	}

	if len(clusters) > 0 {
		aggs, _ := s.DB.ClusterAggregates(ctx)
		pendingKeys := s.DB.PendingHostKeyCount(ctx)
		var risk []dashboardRiskCluster
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
		d.RiskClusters = risk
	}

	writeProto(w, http.StatusOK, d.toProto())
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
