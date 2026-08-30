package api

import (
	"context"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// driftService implements pb.DriftServiceServer
// (proto/nagipath/api/v1/drift.proto). Ports getDrift/getDriftReview/
// postDriftRecompute/postDriftIgnore/postDriftUnignore/postDriftGolden
// (formerly drift.go); instanceWithDrift, clusterGroup, buildDriftWorkQueue
// and the rest of drift.go's helpers stay there, unchanged.
type driftService struct {
	pb.UnimplementedDriftServiceServer
	s *Server
}

func (c *driftService) GetDrift(ctx context.Context, req *pb.GetDriftRequest) (*pb.DriftResponse, error) {
	s := c.s
	resp := &pb.DriftResponse{ScopeFilter: req.Scope, Baseline: req.Baseline}
	for _, b := range store.BaselineLabels {
		resp.Baselines = append(resp.Baselines, &pb.DriftBaselineOption{Kind: b.Kind, Label: b.Label})
	}

	clusters, err := s.DB.Clusters(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	for _, cl := range clusters {
		resp.Clusters = append(resp.Clusters, &pb.DriftClusterOption{Id: cl.ID, Name: cl.Name, Members: int32(cl.Members)})
	}

	scope := req.Cluster
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
	for _, cl := range clusters {
		if cl.ID == clusterID {
			resp.ClusterName = cl.Name
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
			ClusterId: g.ClusterID, ClusterName: g.ClusterName, BaselineName: g.BaselineName,
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

	return resp, nil
}

func (c *driftService) GetDriftReview(ctx context.Context, req *pb.GetDriftReviewRequest) (*pb.DriftReviewResponse, error) {
	s := c.s
	instanceID := req.InstanceId
	inst, err := s.DB.Instance(ctx, instanceID)
	if err != nil {
		return nil, status.Error(codes.NotFound, "No such instance.")
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
		return nil, status.Error(codes.NotFound, "No drift run found for this instance.")
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
		ClusterId: inst.ClusterID.Int64,
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

	return resp, nil
}

func (c *driftService) RecomputeDrift(ctx context.Context, req *pb.RecomputeDriftRequest) (*pb.RecomputeDriftResponse, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	s := c.s
	all := req.Cluster == "all"
	id, _ := strconv.ParseInt(req.Cluster, 10, 64)
	if id == 0 && !all {
		return nil, status.Error(codes.InvalidArgument, "Pick a scope to re-diff.")
	}
	u := userOf(ctx)
	s.DB.Audit(ctx, &u.ID, "drift.recompute", "cluster", &id, req.Baseline)
	stored, skipped := s.DB.RecomputeCluster(ctx, id)
	return &pb.RecomputeDriftResponse{Ok: true, Compared: int32(stored), Skipped: skipped}, nil
}

func (c *driftService) IgnoreDrift(ctx context.Context, req *pb.IgnoreDriftRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	rule := store.IgnoreRule{
		ClusterID: req.Cluster, ObjectKind: strings.TrimSpace(req.ObjectKind),
		Field: strings.TrimSpace(req.Field), Pattern: strings.TrimSpace(req.Pattern),
		Reason: strings.TrimSpace(req.Reason),
	}
	if _, err := c.s.DB.AddIgnoreRule(ctx, rule, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}

func (c *driftService) UnignoreDrift(ctx context.Context, req *pb.UnignoreDriftRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	u := userOf(ctx)
	if err := c.s.DB.DeleteIgnoreRule(ctx, req.Id, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}

// SetGoldenPeer declares a golden peer. Declared, not detected: nagipath has
// no opinion which side of a divergence is correct, so a human names the
// reference. Used by both /drift and ClustersPage's "Change baseline".
func (c *driftService) SetGoldenPeer(ctx context.Context, req *pb.SetGoldenPeerRequest) (*pb.Ok, error) {
	if err := requireAdminRPC(ctx); err != nil {
		return nil, err
	}
	var inst *int64
	if req.Instance > 0 {
		inst = &req.Instance
	}
	u := userOf(ctx)
	if err := c.s.DB.SetGoldenPeer(ctx, req.Cluster, inst, &u.ID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &pb.Ok{Ok: true}, nil
}
