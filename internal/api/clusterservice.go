package api

import (
	"context"
	"sort"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// clusterService implements pb.ClusterServiceServer
// (proto/nagipath/api/v1/clusters.proto's ClusterService) — the domain's
// whole HTTP surface now: GET /clusters and POST /clusters/rename are routed
// here by the service's google.api.http options, not by a net/http
// registration (see api.go's mountGateways).
//
// Registered in-process (RegisterClusterServiceHandlerServer, api.go) rather
// than behind a real gRPC listener: nagipath is a single binary with exactly
// one HTTP caller worth optimising for (its own SPA, plus external
// Bearer-token callers at the same /api/ path — internal/web.Server's doc
// comment), so a second network listener and a loopback grpc.Dial back into
// the same process would buy nothing. requireAuth still runs as an ordinary
// http.Handler wrapped around the whole gateway mux (api.go), and its
// context value survives grpc-gateway's AnnotateIncomingContext because that
// call wraps req.Context() rather than replacing it.
// ponytail: a real grpc.Server with an auth interceptor is the textbook
// setup and the upgrade path the day an actual non-HTTP gRPC client needs
// this service; until then this is simpler for the same behaviour.
type clusterService struct {
	pb.UnimplementedClusterServiceServer
	s *Server
}

// ListClusters ports getClusters' body (formerly clusters.go, pre-gateway):
// same discovery call, same filters (q/drift/sort) and sort orders, same
// optional selected-cluster detail (cluster=) — argument fields now, not
// query values, mapped 1:1 by grpc-gateway's PopulateQueryParameters.
func (c *clusterService) ListClusters(ctx context.Context, req *pb.ListClustersRequest) (*pb.ClustersResponse, error) {
	s := c.s
	if err := s.DB.ReconcileClusters(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	resp := &pb.ClustersResponse{Query: strings.TrimSpace(req.Q), DriftFilter: req.Drift, Sort: req.Sort}
	if resp.DriftFilter == "" {
		resp.DriftFilter = "any"
	}
	if resp.Sort == "" {
		resp.Sort = "drift"
	}

	all, err := s.DB.Clusters(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	aggs, err := s.DB.ClusterAggregates(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	members := map[int64][]store.Instance{}
	resp.Total = int32(len(instances))
	for _, in := range instances {
		if in.ClusterID.Valid {
			members[in.ClusterID.Int64] = append(members[in.ClusterID.Int64], in)
		}
	}

	for _, cl := range all {
		mem := members[cl.ID]
		if len(mem) == 0 {
			continue
		}
		row := &pb.ClusterListItem{Id: cl.ID, Name: cl.Name, Members: int32(cl.Members),
			Vendor: mem[0].Vendor, GoldenPeerName: cl.GoldenPeerName,
			InstancesCollected: int32(len(mem))}
		if agg, ok := aggs[cl.ID]; ok {
			row.DriftCount = int32(agg.DriftCount)
			row.CertsExpiring_30D = int32(agg.CertsExpiring30d)
			if agg.LastCollected.Valid {
				row.LastCollected = agg.LastCollected.String
			}
		}

		if resp.Query != "" && !strings.Contains(strings.ToLower(cl.Name), strings.ToLower(resp.Query)) {
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

	if req.Cluster != 0 {
		for _, cl := range all {
			if cl.ID != req.Cluster {
				continue
			}
			mems, err := s.DB.ClusterMembers(ctx, req.Cluster)
			if err != nil {
				return nil, status.Error(codes.Unavailable, err.Error())
			}
			det := &pb.ClusterDetail{Id: cl.ID, Name: cl.Name, Members: int32(cl.Members), GoldenPeerName: cl.GoldenPeerName}
			for _, m := range mems {
				item := &pb.ClusterMemberItem{Id: m.ID, DisplayName: m.DisplayName,
					IsGolden: cl.GoldenPeer.Valid && cl.GoldenPeer.Int64 == m.ID}
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

	return resp, nil
}

// RenameCluster ports postRenameCluster's body. No net/http-level
// requireAdmin wrapping this specific RPC (the gateway mux is wrapped with
// requireAuth as a whole, api.go) — the admin check happens here, against
// the user requireAuth already put on ctx, the same way any other
// permission check a use case owns would.
func (c *clusterService) RenameCluster(ctx context.Context, req *pb.RenameClusterRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	if err := c.s.DB.RenameCluster(ctx, req.Cluster, req.Name, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}
