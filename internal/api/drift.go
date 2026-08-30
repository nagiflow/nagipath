package api

import (
	"fmt"
	"sort"
	"strings"

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
	ClusterID      int64
	ClusterName    string
	BaselineName   string
	NodesWithDrift int
	TotalNodes     int
	Instances      []instanceWithDrift
}

// getDrift and getDriftReview moved to driftservice.go as DriftService's
// GetDrift and GetDriftReview RPCs (docs/adr/0018,
// proto/nagipath/api/v1/drift.proto).

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
			g = &clusterGroup{ClusterID: cid}
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

// getDriftReview, postDriftRecompute, postDriftIgnore, postDriftUnignore and
// postDriftGolden moved to driftservice.go as DriftService's GetDriftReview/
// RecomputeDrift/IgnoreDrift/UnignoreDrift/SetGoldenPeer RPCs
// (docs/adr/0018, proto/nagipath/api/v1/drift.proto).
