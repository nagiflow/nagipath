package api

import (
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

// getDashboard moved to dashboardservice.go as DashboardService's
// GetDashboard RPC (docs/adr/0018, proto/nagipath/api/v1/dashboard.proto).

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
