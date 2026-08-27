package web

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

type clusterRow struct {
	store.Cluster
	Instances        []store.Instance
	Nodes            []string
	Vendor           string
	DriftCount       int
	CertsExpiring30d int
	LastCollected    string
}

type clustersData struct {
	Clusters   []clusterRow
	Unassigned []store.Instance
	Total      int
	Sel        *clusterDetail

	// Filters.
	Query       string
	DriftFilter string // "any", "drifted", "clean"
	Sort        string // "drift", "name", "certs", "collected"
}

type clusterDetail struct {
	Cluster store.Cluster
	Members []store.ClusterMember
	// Vendor and LastCollected are aggregates over Members, computed here rather
	// than reached for in the template — `index .Members 0` panics on a cluster
	// whose instances have all been retired, and the template has no way to say
	// "the oldest of these".
	Vendor        string
	LastCollected string
}

// summarize fills the two aggregates. LastCollected is the *oldest* member, not
// the newest: a cluster is only as fresh as its stalest node, and the newest
// would hide exactly the node an operator needs to see. Vendor is left empty
// when members disagree, because one cluster reading "nginx" while a member runs
// haproxy is worse than no answer.
func (d *clusterDetail) summarize() {
	for _, m := range d.Members {
		switch {
		case d.Vendor == "":
			d.Vendor = m.Vendor
		case d.Vendor != m.Vendor:
			d.Vendor = "mixed"
		}
		if !m.LastCaptured.Valid {
			continue
		}
		if d.LastCollected == "" || m.LastCaptured.String < d.LastCollected {
			d.LastCollected = m.LastCaptured.String
		}
	}
}

// clusters is the level above the node: a set of instances running byte-identical
// configuration, discovered rather than declared.
//
// ponytail: discovery re-runs when this page is opened, which keeps the grouping
// true after any collection without a scheduler to own it — it moved here from
// /fleet with the rest of that screen. Move it to the end of a collection when a
// fleet is big enough for the delay to be felt.
func (s *Server) clusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.DB.ReconcileClusters(ctx); err != nil {
		s.Log.Warn("cluster discovery", "err", err)
	}

	// Parse filters.
	d := clustersData{}
	d.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	d.DriftFilter = r.URL.Query().Get("drift")
	if d.DriftFilter == "" {
		d.DriftFilter = "any"
	}
	d.Sort = r.URL.Query().Get("sort")
	if d.Sort == "" {
		d.Sort = "drift"
	}

	all, err := s.DB.Clusters(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	aggs, err := s.DB.ClusterAggregates(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	members := map[int64][]store.Instance{}
	d.Total = len(instances)
	for _, in := range instances {
		if in.ClusterID.Valid {
			members[in.ClusterID.Int64] = append(members[in.ClusterID.Int64], in)
			continue
		}
		d.Unassigned = append(d.Unassigned, in)
	}
	// A cluster with no members is a name kept for a group that has diverged. It is
	// not a thing in the fleet right now, so it is not listed as one.
	for _, c := range all {
		if len(members[c.ID]) == 0 {
			continue
		}
		row := clusterRow{Cluster: c, Instances: members[c.ID]}
		row.Vendor = members[c.ID][0].Vendor
		seen := map[string]bool{}
		for _, in := range members[c.ID] {
			if !seen[in.NodeDisplayName] {
				seen[in.NodeDisplayName] = true
				row.Nodes = append(row.Nodes, in.NodeDisplayName)
			}
		}
		if agg, ok := aggs[c.ID]; ok {
			row.DriftCount = agg.DriftCount
			row.CertsExpiring30d = agg.CertsExpiring30d
			if agg.LastCollected.Valid {
				row.LastCollected = agg.LastCollected.String
			}
		}

		// Apply filters.
		if d.Query != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(d.Query)) {
			continue
		}
		if d.DriftFilter == "drifted" && row.DriftCount == 0 {
			continue
		}
		if d.DriftFilter == "clean" && row.DriftCount > 0 {
			continue
		}

		d.Clusters = append(d.Clusters, row)
	}

	// Apply sorting.
	sort.Slice(d.Clusters, func(i, j int) bool {
		switch d.Sort {
		case "name":
			return d.Clusters[i].Name < d.Clusters[j].Name
		case "certs":
			if d.Clusters[i].CertsExpiring30d != d.Clusters[j].CertsExpiring30d {
				return d.Clusters[i].CertsExpiring30d > d.Clusters[j].CertsExpiring30d
			}
			return d.Clusters[i].Name < d.Clusters[j].Name
		case "collected":
			if d.Clusters[i].LastCollected != d.Clusters[j].LastCollected {
				return d.Clusters[i].LastCollected > d.Clusters[j].LastCollected
			}
			return d.Clusters[i].Name < d.Clusters[j].Name
		default: // "drift"
			if d.Clusters[i].DriftCount != d.Clusters[j].DriftCount {
				return d.Clusters[i].DriftCount > d.Clusters[j].DriftCount
			}
			return d.Clusters[i].Name < d.Clusters[j].Name
		}
	})

	// If a cluster is selected, fetch its members with divergence counts. A failed
	// query is an error, not an empty panel: falling back to "Pick a cluster" tells
	// an operator who just picked one that they did not.
	if idStr := r.URL.Query().Get("cluster"); idStr != "" {
		if selID, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			for _, c := range all {
				if c.ID == selID {
					mems, err := s.DB.ClusterMembers(ctx, selID)
					if err != nil {
						s.serverError(w, r, err)
						return
					}
					d.Sel = &clusterDetail{Cluster: c, Members: mems}
					d.Sel.summarize()
					break
				}
			}
		}
	}

	s.render(w, r, "clusters.html", "Clusters", d)
}

// Clusters are discovered from the configuration itself, so there is no form for
// creating one and none for membership. The name is the only editable part.
func (s *Server) renameCluster(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	id, _ := strconv.ParseInt(r.FormValue("cluster"), 10, 64)
	if err := s.DB.RenameCluster(ctx, id, r.FormValue("name"), &u.ID); err != nil {
		redirect(w, r, "/clusters", "", err.Error())
		return
	}
	redirect(w, r, "/clusters", "cluster renamed", "")
}
