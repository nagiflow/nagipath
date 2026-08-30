package api

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// dashboardService implements pb.DashboardServiceServer
// (proto/nagipath/api/v1/dashboard.proto). Ports getDashboard (formerly the
// bulk of dashboard.go); dashboardBuild and its helpers stay in dashboard.go.
type dashboardService struct {
	pb.UnimplementedDashboardServiceServer
	s *Server
}

// GetDashboard is the fleet-overview screen: counts, the needs-attention
// list, the 24h activity strip and a per-cluster risk ranking. Ported field
// for field from internal/web/dashboard.go's dashboard() — same queries,
// same filters (cluster/severity/attention_cluster), same tone/risk rules.
func (c *dashboardService) GetDashboard(ctx context.Context, req *pb.GetDashboardRequest) (*pb.DashboardResponse, error) {
	s := c.s
	d := dashboardBuild{SelectedCluster: req.Cluster, AttentionSeverity: req.Severity, AttentionCluster: req.AttentionCluster}
	if d.AttentionSeverity == "" {
		d.AttentionSeverity = "all"
	}

	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	d.Clusters = clusters

	allNodes, err := s.DB.Nodes(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	allInstances, err := s.DB.Instances(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
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
	for _, cv := range certs {
		if cv.NotAfter != "" && cv.NotAfter <= cutoff {
			d.Certs30++
			d.CertBindings += cv.Bindings
			cn := cv.SubjectCN
			if strings.TrimSpace(cn) == "" {
				cn = "(no CN)"
			}
			d.Expiring = append(d.Expiring, cv)
			attention = append(attention, dashboardAttention{Kind: "CERT", Tone: attentionTone("CERT"),
				Text: cn, Note: fmt.Sprintf("%d binding(s)", cv.Bindings),
				Link: fmt.Sprintf("/certificates?cert=%d", cv.ID), Since: cv.NotAfter})
		}
	}

	// design/'s 2j reports license state as a needs-attention row rather than a
	// banner over every page.
	if s.LicenseStatus != nil {
		if _, notice := s.LicenseStatus(ctx); notice != "" {
			attention = append(attention, dashboardAttention{Kind: "LICENSE", Tone: attentionTone("LICENSE"),
				Text: notice, Note: fmt.Sprintf("%d nodes in inventory", len(allNodes)),
				Link: "/settings/license"})
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

	return d.toProto(), nil
}
