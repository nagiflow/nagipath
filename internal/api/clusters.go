package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// getClusters ports internal/web/clusters.go's clusters() — same discovery
// call, same filters (?q=, ?drift=, ?sort=) and sort orders, same optional
// selected-cluster detail panel (?cluster=) — as
// proto/nagipath/api/v1/clusters.proto's ClustersResponse (docs/adr/0018).
func (s *Server) getClusters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.DB.ReconcileClusters(ctx); err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	q := r.URL.Query()
	resp := &pb.ClustersResponse{}
	resp.Query = strings.TrimSpace(q.Get("q"))
	resp.DriftFilter = q.Get("drift")
	if resp.DriftFilter == "" {
		resp.DriftFilter = "any"
	}
	resp.Sort = q.Get("sort")
	if resp.Sort == "" {
		resp.Sort = "drift"
	}

	all, err := s.DB.Clusters(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	aggs, err := s.DB.ClusterAggregates(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	members := map[int64][]store.Instance{}
	resp.Total = int32(len(instances))
	for _, in := range instances {
		if in.ClusterID.Valid {
			members[in.ClusterID.Int64] = append(members[in.ClusterID.Int64], in)
		}
	}

	for _, c := range all {
		mem := members[c.ID]
		if len(mem) == 0 {
			continue
		}
		row := &pb.ClusterListItem{Id: c.ID, Name: c.Name, Members: int32(c.Members),
			Vendor: mem[0].Vendor, GoldenPeerName: c.GoldenPeerName,
			InstancesCollected: int32(len(mem))}
		if agg, ok := aggs[c.ID]; ok {
			row.DriftCount = int32(agg.DriftCount)
			row.CertsExpiring_30D = int32(agg.CertsExpiring30d)
			if agg.LastCollected.Valid {
				row.LastCollected = agg.LastCollected.String
			}
		}

		if resp.Query != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(resp.Query)) {
			continue
		}
		if resp.DriftFilter == "drifted" && row.DriftCount == 0 {
			continue
		}
		if resp.DriftFilter == "clean" && row.DriftCount > 0 {
			continue
		}
		resp.Clusters = append(resp.Clusters, row)
	}

	sort.Slice(resp.Clusters, func(i, j int) bool {
		a, b := resp.Clusters[i], resp.Clusters[j]
		switch resp.Sort {
		case "name":
			return a.Name < b.Name
		case "certs":
			if a.CertsExpiring_30D != b.CertsExpiring_30D {
				return a.CertsExpiring_30D > b.CertsExpiring_30D
			}
		case "collected":
			if a.LastCollected != b.LastCollected {
				return a.LastCollected > b.LastCollected
			}
		default: // "drift"
			if a.DriftCount != b.DriftCount {
				return a.DriftCount > b.DriftCount
			}
		}
		return a.Name < b.Name
	})

	if idStr := q.Get("cluster"); idStr != "" {
		if selID, err := strconv.ParseInt(idStr, 10, 64); err == nil {
			for _, c := range all {
				if c.ID != selID {
					continue
				}
				mems, err := s.DB.ClusterMembers(ctx, selID)
				if err != nil {
					apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
					return
				}
				det := &pb.ClusterDetail{Id: c.ID, Name: c.Name, Members: int32(c.Members),
					GoldenPeerName: c.GoldenPeerName}
				for _, m := range mems {
					item := &pb.ClusterMemberItem{Id: m.ID, DisplayName: m.DisplayName,
						IsGolden: c.GoldenPeer.Valid && c.GoldenPeer.Int64 == m.ID}
					if m.Divergence.Valid {
						v := m.Divergence.Int64
						item.Divergence = &v
					}
					det.MemberList = append(det.MemberList, item)
					switch {
					case det.Vendor == "":
						det.Vendor = m.Vendor
					case det.Vendor != m.Vendor:
						det.Vendor = "mixed"
					}
					if m.LastCaptured.Valid && (det.LastCollected == "" || m.LastCaptured.String < det.LastCollected) {
						det.LastCollected = m.LastCaptured.String
					}
				}
				resp.Selected = det
				break
			}
		}
	}

	writeProto(w, http.StatusOK, resp)
}

// postRenameCluster ports internal/web/clusters.go's renameCluster(): the
// only editable field a discovered cluster has. Not a proto message on
// either side — a rename is a fire-and-forget mutation, not a resource with
// a shape worth a generated type (see client.ts's postAction).
func (s *Server) postRenameCluster(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Cluster int64  `json:"cluster"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	u := userOf(r)
	if err := s.DB.RenameCluster(r.Context(), body.Cluster, body.Name, &u.ID); err != nil {
		apiError(w, http.StatusUnprocessableEntity, "rename_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
