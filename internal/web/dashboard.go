package web

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

// ---------------------------------------------------------------- dashboard

// activityBucket is one column of the collection-activity strip: a window of the
// last day and what happened in it. There is no chart library and no axis —
// the count is printed beside the shape.
type activityBucket struct {
	Label    string
	Total    int
	Failed   int
	Degraded int
}

// subject names an instance the way an operator does: the host first. Its
// DisplayName is vendor + config basename, so every nginx in the fleet reads
// "nginx nginx.conf" and three rows of it name nothing.
func subject(in store.Instance) string {
	if in.NodeDisplayName == "" {
		return in.DisplayName
	}
	return in.NodeDisplayName + " · " + in.DisplayName
}

// attention is one row of "needs attention". Kind is the badge word, and every
// row has a link, because a finding an operator cannot open is noise.
type attention struct {
	Kind    string
	Text    string
	Note    string
	Link    string
	Cluster string
	Since   string
}

type dashboardData struct {
	Nodes     int
	Instances int
	Vendors   map[string]int
	Degraded  int
	Pending   int
	// Instance states, so "12 instances" can say how many are actually usable.
	OK, Unparsed, PendingInstances int
	Certs30                        int
	Drifted                        int
	DriftedClusters                int // number of clusters with drift
	// Counts for new tiles.
	FreshInstances   int // instances collected < 6h ago
	AgingInstances   int // instances collected 6–24h ago
	StaleInstances   int // instances not collected for more than 24h
	TotalRules       int
	CertBindings     int // bindings on expiring certs
	UnreachableNodes int
	UnreachableSince string
	VendorCount      int

	Failing      []store.Node
	Collections  []store.Collection
	Expiring     []store.CertificateView
	Activity     []activityBucket
	ActivityMax  int
	Attention    []attention
	RecentTraces []trace.Summary
	RiskClusters []dashboardCluster

	// Scope filter.
	Clusters        []store.Cluster
	SelectedCluster int64 // 0 means "all clusters"

	// Needs attention filters.
	AttentionSeverity string // "all", "err", "deg", "inf"
	AttentionCluster  int64  // 0 means "all"

	// Coverage strip: oldest snapshot.
	OldestSnapshot     string
	OldestSnapshotNode string

	// Collection runs: average duration.
	AvgCollectionDuration float64
}

type dashboardCluster struct {
	ID           int64
	Name         string
	Nodes        int
	Fresh        int
	FreshPercent int // Fresh as percentage
	Drift        int
	Certs        int
	CertsLabel   string // "N exp" or "—"
	State        string
	Risk         int
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := dashboardData{Vendors: map[string]int{}}

	// Parse filters from URL.
	if cid := r.URL.Query().Get("cluster"); cid != "" {
		if id, err := strconv.ParseInt(cid, 10, 64); err == nil {
			d.SelectedCluster = id
		}
	}
	d.AttentionSeverity = r.URL.Query().Get("severity")
	if d.AttentionSeverity == "" {
		d.AttentionSeverity = "all"
	}
	if acid := r.URL.Query().Get("attention_cluster"); acid != "" {
		if id, err := strconv.ParseInt(acid, 10, 64); err == nil {
			d.AttentionCluster = id
		}
	}

	// Fetch clusters for the scope selector.
	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Clusters = clusters

	allNodes, err := s.DB.Nodes(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	allInstances, err := s.DB.Instances(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Apply cluster filter.
	var nodes []store.Node
	var instances []store.Instance
	if d.SelectedCluster == 0 {
		nodes = allNodes
		instances = allInstances
	} else {
		// Filter: nodes with at least one instance in the selected cluster.
		nodeInCluster := make(map[int64]bool)
		for _, in := range allInstances {
			if in.ClusterID.Valid && in.ClusterID.Int64 == d.SelectedCluster {
				instances = append(instances, in)
				nodeInCluster[in.NodeID] = true
			}
		}
		for _, n := range allNodes {
			if nodeInCluster[n.ID] {
				nodes = append(nodes, n)
			}
		}
	}

	quarantineThreshold := s.DB.SettingInt(ctx, "quarantine_after_failures")
	now := time.Now().UTC()
	sixHoursAgo := now.Add(-6 * time.Hour).Format("2006-01-02T15:04:05Z")
	oneDayAgo := now.Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z")
	var earliestUnreachable string
	for _, n := range nodes {
		if n.ConsecutiveFailures > 0 {
			d.Failing = append(d.Failing, n)
			d.UnreachableNodes++
			// Track the earliest unreachable time.
			if n.LastCollection.Valid && (earliestUnreachable == "" || n.LastCollection.String < earliestUnreachable) {
				earliestUnreachable = n.LastCollection.String
			}
			// The product's own word, not a symptom. "NO SSH" describes what we
			// observed; "QUARANTINED" is the state the node is actually in, it is
			// what /nodes and the node page both call it, and it is the word an
			// operator searches for when they want to know why collection stopped.
			kind := "DEGRADED"
			if store.Quarantined(n.ConsecutiveFailures, quarantineThreshold) {
				kind = "QUARANTINED"
			}
			d.Attention = append(d.Attention, attention{
				Kind:    kind,
				Text:    n.DisplayName,
				Note:    fmt.Sprintf("%d consecutive failures", n.ConsecutiveFailures),
				Link:    fmt.Sprintf("/nodes/%d", n.ID),
				Cluster: "",
				Since:   n.LastCollection.String,
			})
		}
	}
	if earliestUnreachable != "" {
		d.UnreachableSince = earliestUnreachable
	}
	d.Nodes = len(nodes)
	d.Instances = len(instances)
	for _, in := range instances {
		d.Vendors[in.Vendor]++
		// Count instances collected in the last 6 hours.
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
		switch in.State() {
		case "ok":
			d.OK++
		case "pending":
			d.PendingInstances++
		case "unparsed":
			d.Unparsed++
			d.Attention = append(d.Attention, attention{
				Kind:    "DEGRADED",
				Text:    subject(in),
				Note:    "parse " + in.ParseState,
				Link:    fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName,
				Since:   in.LastCaptured.String,
			})
		case "degraded":
			d.Degraded++
			d.Attention = append(d.Attention, attention{
				Kind:    "DEGRADED",
				Text:    subject(in),
				Note:    "incomplete snapshot",
				Link:    fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName,
				Since:   in.LastCaptured.String,
			})
		}
		// An instance with no derivable access log can never be raised past
		// observed_effect, which is a log-format gap and not a fault.
		if len(in.AccessLogPaths) == 0 && in.State() != "pending" {
			d.Attention = append(d.Attention, attention{
				Kind:    "LOG FORMAT",
				Text:    subject(in),
				Note:    "no access log path derivable",
				Link:    fmt.Sprintf("/instances/%d", in.ID),
				Cluster: in.ClusterName,
				Since:   in.LastCaptured.String,
			})
		}
	}
	d.VendorCount = len(d.Vendors)

	// STALE row: nodes not collected in 24h.
	staleCount, _ := s.DB.StaleNodeCount(ctx, oneDayAgo)
	if staleCount > 0 {
		d.Attention = append(d.Attention, attention{
			Kind:    "STALE",
			Text:    fmt.Sprintf("%d nodes not collected in 24h", staleCount),
			Note:    "",
			Link:    "/nodes",
			Cluster: "",
			Since:   oneDayAgo,
		})
	}

	// Count total rules across all current snapshots. This requires a separate query
	// since Rule count is not denormalized on Instance.
	if ruleCount, err := s.DB.RuleCount(ctx); err == nil {
		d.TotalRules = ruleCount
	}
	d.Degraded += d.Unparsed
	d.Pending = s.DB.PendingHostKeyCount(ctx)
	if d.Pending > 0 {
		d.Attention = append(d.Attention, attention{
			Kind:    "HOST KEY",
			Text:    fmt.Sprintf("%d host key(s) waiting", d.Pending),
			Note:    "",
			Link:    "/onboarding",
			Cluster: "",
			Since:   "",
		})
	}
	d.Collections, _ = s.DB.Collections(ctx, 200)
	d.Activity, d.ActivityMax = activity(time.Now(), d.Collections)
	if len(d.Collections) > 5 {
		d.Collections = d.Collections[:5]
	}

	// Average collection duration.
	d.AvgCollectionDuration, _ = s.DB.AvgCollectionDuration(ctx, oneDayAgo)

	// Oldest snapshot for coverage strip.
	d.OldestSnapshot, d.OldestSnapshotNode, _ = s.DB.OldestSnapshot(ctx)

	// Drift is counted per instance, not per finding: "three hosts diverge" is the
	// decision, and "forty-one differences" is the reading afterwards.
	if runs, err := s.DB.DriftRuns(ctx, 0, ""); err == nil {
		cluster := map[int64]string{}
		clusterID := map[int64]int64{}
		for _, in := range instances {
			cluster[in.ID] = in.ClusterName
			if in.ClusterID.Valid {
				clusterID[in.ID] = in.ClusterID.Int64
			}
		}
		seen := map[int64]bool{}
		clustersWithDrift := map[int64]bool{}
		for _, run := range runs {
			if run.FindingCount > 0 && !seen[run.InstanceID] {
				seen[run.InstanceID] = true
				d.Drifted++
				if cid, ok := clusterID[run.InstanceID]; ok {
					clustersWithDrift[cid] = true
				}
				d.Attention = append(d.Attention, attention{
					Kind:    "DRIFT",
					Text:    run.NodeName + " · " + run.InstanceName,
					Note:    fmt.Sprintf("%d divergence(s)", run.FindingCount),
					Link:    "/drift",
					Cluster: cluster[run.InstanceID],
					Since:   run.ComputedAt,
				})
			}
		}
		d.DriftedClusters = len(clustersWithDrift)
	}

	// Certificates within 30 days, because that is the window in which an operator
	// can still do something about it.
	certs, _ := s.DB.Certificates(ctx)
	cutoff := time.Now().UTC().AddDate(0, 0, 30).Format("2006-01-02T15:04:05Z")
	for _, c := range certs {
		if c.NotAfter != "" && c.NotAfter <= cutoff {
			d.Expiring = append(d.Expiring, c)
			d.Certs30++
			d.CertBindings += c.Bindings
			d.Attention = append(d.Attention, attention{
				Kind:    "CERT",
				Text:    orText(c.SubjectCN, "(no CN)"),
				Note:    fmt.Sprintf("%d binding(s)", c.Bindings),
				Link:    fmt.Sprintf("/certificates?cert=%d", c.ID),
				Cluster: "",
				Since:   c.NotAfter,
			})
		}
	}

	// Apply attention filters.
	filtered := make([]attention, 0, len(d.Attention))
	for _, a := range d.Attention {
		// Severity filter.
		if d.AttentionSeverity != "all" {
			tone := attentionTone(a.Kind)
			if tone != d.AttentionSeverity {
				continue
			}
		}
		// Cluster filter.
		if d.AttentionCluster != 0 {
			// Match cluster by name since attention rows may have cluster names.
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
		filtered = append(filtered, a)
	}
	d.Attention = filtered

	// Recent traces for the new panel.
	d.RecentTraces, _ = trace.Recent(ctx, s.DB, 5)

	// Cluster risk is a compact projection of data already used by /clusters.
	// The dashboard sorts actionable groups first: collection freshness, drift,
	// then certificate expiry. It does not invent a score.
	if len(clusters) > 0 {
		aggs, _ := s.DB.ClusterAggregates(ctx)
		pendingKeys := s.DB.PendingHostKeyCount(ctx)
		for _, cl := range clusters {
			if cl.Members == 0 {
				continue
			}
			row := dashboardCluster{ID: cl.ID, Name: cl.Name, Nodes: cl.Members, State: "ok"}
			for _, in := range allInstances {
				if in.ClusterID.Valid && in.ClusterID.Int64 == cl.ID &&
					in.LastCaptured.Valid && in.LastCaptured.String >= sixHoursAgo {
					row.Fresh++
				}
			}
			if agg, ok := aggs[cl.ID]; ok {
				row.Drift, row.Certs = agg.DriftCount, agg.CertsExpiring30d
			}
			// Compute Fresh as percentage.
			if row.Nodes > 0 {
				row.FreshPercent = (row.Fresh * 100) / row.Nodes
			}
			// Certs label: "N exp" or "—".
			if row.Certs > 0 {
				row.CertsLabel = fmt.Sprintf("%d exp", row.Certs)
			} else {
				row.CertsLabel = "—"
			}
			// State column: FAILING when collection is failing, DRIFT when it has drift,
			// PENDING n when it has host keys awaiting approval, OK otherwise.
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
			d.RiskClusters = append(d.RiskClusters, row)
		}
		sort.Slice(d.RiskClusters, func(i, j int) bool {
			if d.RiskClusters[i].Risk != d.RiskClusters[j].Risk {
				return d.RiskClusters[i].Risk > d.RiskClusters[j].Risk
			}
			return d.RiskClusters[i].Name < d.RiskClusters[j].Name
		})
		if len(d.RiskClusters) > 5 {
			d.RiskClusters = d.RiskClusters[:5]
		}
	}

	s.render(w, r, "dashboard.html", "Dashboard", d)
}

// attentionTone is the badge tone for one "needs attention" kind. It lives here
// rather than as an if-chain in the template because the severity filter and the
// badge have to agree: a row the filter calls an error and the badge tints amber
// is a row an operator cannot count.
func attentionTone(kind string) string {
	switch kind {
	case "QUARANTINED":
		return "err"
	case "DEGRADED", "CERT", "STALE", "DRIFT":
		return "deg"
	}
	return "inf"
}

func orText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// activity buckets collections into hourly windows over the last 24h. The wireframe
// uses 1h buckets, not the previous 3h span; a bucket with degraded runs renders in
// amber so the colour matches the legend beside it.
//
// ponytail: fixed buckets computed in Go over the rows the dashboard has already read.
// A GROUP BY would be the upgrade when the window becomes a control rather than the
// fixed 24h the wireframe asks for.
// `now` is a parameter rather than a call to time.Now inside, because otherwise
// which bucket a fixture lands in depends on what minute the test suite runs at.
func activity(now time.Time, list []store.Collection) ([]activityBucket, int) {
	const buckets, span = 24, time.Hour
	// Boundaries sit on the hour, and the last bucket is the current hour, so a
	// collection a minute ago lands in it. Anchoring the window to the current
	// *minute* instead put a bar labelled 14:00 over the span 14:37–15:37.
	now = now.UTC().Truncate(span)
	oldest := now.Add(-(buckets - 1) * span)
	out := make([]activityBucket, buckets)
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
			// A target clock running ahead still belongs at the right edge.
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
