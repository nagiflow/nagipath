package api

import (
	"context"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// collectionService implements pb.CollectionServiceServer
// (proto/nagipath/api/v1/settings.proto). Ports getCollections (formerly
// collections.go); collectionSince, collectionOutcome and
// collectionsPageSize stay in collections.go since getCollectionsCSV still
// needs them too.
type collectionService struct {
	pb.UnimplementedCollectionServiceServer
	s *Server
}

func (c *collectionService) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.CollectionsResponse, error) {
	s := c.s
	node := strings.TrimSpace(req.Node)
	rng := req.Range
	switch rng {
	case "7d", "30d", "90d":
	default:
		rng = "24h"
	}

	since := collectionSince(rng)
	all, err := s.DB.CollectionsSince(ctx, since)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	nodes, err := s.DB.Nodes(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp := &pb.CollectionsResponse{Empty: len(nodes) == 0}
	if resp.Empty {
		return resp, nil
	}

	rows := collectionRowsFrom(all)

	if node != "" {
		nodelow := strings.ToLower(node)
		var kept []collectionRow
		for _, rr := range rows {
			if strings.Contains(strings.ToLower(rr.nodeName), nodelow) {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}
	if req.Status != "" {
		var kept []collectionRow
		for _, rr := range rows {
			if rr.status == req.Status {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}
	if req.Trigger != "" {
		var kept []collectionRow
		for _, rr := range rows {
			if rr.trigger == req.Trigger {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}

	stats, err := s.DB.CollectionStatsFor(ctx, since, node, req.Status, req.Trigger)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp.Total, resp.Succeeded, resp.Degraded = int32(stats.Runs), int32(stats.Succeeded), int32(stats.Degraded)
	resp.Failed, resp.Running = int32(stats.Failed), int32(stats.Running)
	resp.MedianMs, resp.P95Ms = stats.MedianMS, stats.P95MS

	cursor, _ := strconv.Atoi(req.Cursor)
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	end := cursor + collectionsPageSize
	if end > len(rows) {
		end = len(rows)
	} else {
		resp.HasMore = true
		resp.NextCursor = strconv.Itoa(end)
	}
	page := rows[cursor:end]
	if len(page) > 0 {
		resp.From, resp.To = int32(cursor+1), int32(end)
	}
	for _, rr := range page {
		resp.Rows = append(resp.Rows, &pb.CollectionRow{
			Id: rr.id, NodeId: rr.nodeID, NodeName: rr.nodeName, Trigger: rr.trigger,
			StartedAt: rr.startedAt, Status: rr.status, Error: rr.errStr,
			InstancesSeen: int32(rr.instancesSeen), DurationMs: rr.durationMS,
			Outcome: collectionOutcome(rr.errStr, rr.instancesSeen),
		})
	}
	return resp, nil
}
