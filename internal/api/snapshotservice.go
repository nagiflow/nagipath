package api

import (
	"context"
	"strconv"
	"strings"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// snapshotService implements pb.SnapshotServiceServer
// (proto/nagipath/api/v1/snapshots.proto). Ports getSnapshots/
// getSnapshotFile (formerly snapshots.go); snapshotRow, rangeSince and
// snapshotsPageSize stay in snapshots.go since getSnapshotsCSV still needs
// them too.
type snapshotService struct {
	pb.UnimplementedSnapshotServiceServer
	s *Server
}

func (c *snapshotService) ListSnapshots(ctx context.Context, req *pb.ListSnapshotsRequest) (*pb.SnapshotsListResponse, error) {
	s := c.s
	resp := &pb.SnapshotsListResponse{
		Query: strings.TrimSpace(req.Q), Range: req.Range,
		Changes: req.Changes, Trigger: req.Trigger,
	}
	switch resp.Range {
	case "7d", "30d", "90d":
	default:
		resp.Range = "24h"
	}
	resp.RetentionDays = int32(s.DB.SettingInt(ctx, "snapshot_retention_days"))

	all, err := s.DB.SnapshotsSince(ctx, rangeSince(resp.Range))
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	resp.Empty = len(instances) == 0
	if resp.Empty {
		return resp, nil
	}

	rows := snapshotRowsFrom(all, resp)

	if resp.Query != "" {
		qlow := strings.ToLower(resp.Query)
		var kept []snapshotRow
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.Instance), qlow) ||
				strings.Contains(strings.ToLower(row.Node), qlow) ||
				strings.Contains(strings.ToLower(row.Cluster), qlow) {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	switch resp.Changes {
	case "yes", "no":
		want := resp.Changes == "yes"
		var kept []snapshotRow
		for _, row := range rows {
			if row.Changed == want {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if resp.Trigger != "" {
		var kept []snapshotRow
		for _, row := range rows {
			if row.Trigger == resp.Trigger {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	resp.Filtered = int32(len(rows))

	cursor, _ := strconv.Atoi(req.Cursor)
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	end := cursor + snapshotsPageSize
	if end > len(rows) {
		end = len(rows)
	} else {
		resp.HasMore = true
		resp.NextCursor = strconv.Itoa(end)
	}
	for _, row := range rows[cursor:end] {
		resp.List = append(resp.List, &pb.SnapshotRow{
			Id: row.ID, InstanceId: row.InstanceID, Instance: row.Instance, Node: row.Node,
			Cluster: row.Cluster, CapturedAt: row.CapturedAt, Trigger: row.Trigger,
			Changed: row.Changed, BytesRaw: row.BytesRaw, State: row.State,
		})
	}
	if len(resp.List) > 0 {
		resp.From, resp.To = int32(cursor+1), int32(end)
	}

	return resp, nil
}

func (c *snapshotService) GetSnapshotFile(ctx context.Context, req *pb.GetSnapshotFileRequest) (*pb.SnapshotFileResponse, error) {
	s := c.s
	snap, err := s.DB.SnapshotByID(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "No such snapshot.")
	}
	files, err := s.DB.SnapshotFiles(ctx, snap.ID)
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	for _, f := range files {
		if f.ID != req.FileId {
			continue
		}
		body, err := s.DB.Blob(ctx, f.Digest)
		if err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
		content := string(body)
		resp := &pb.SnapshotFileResponse{
			Snapshot: &pb.Snapshot{
				Id: snap.ID, CapturedAt: snap.CapturedAt, Degraded: snap.Degraded,
				DegradedReason: snap.DegradedReason, FileCount: int32(snap.FileCount),
				BytesRaw: snap.BytesRaw, ParseState: snap.ParseState,
			},
			File: &pb.FileRef{Id: f.ID, Kind: f.Kind, Path: f.Path, BytesRaw: f.BytesRaw, Truncated: f.Truncated},
			Body: content,
		}
		if inst, err := s.DB.Instance(ctx, snap.InstanceID); err == nil {
			resp.Instance = &pb.Instance{
				Id: inst.ID, NodeId: inst.NodeID, Vendor: inst.Vendor, DisplayName: inst.DisplayName,
			}
		}

		if req.B != "" {
			if offset, err := strconv.Atoi(req.B); err == nil && offset >= 0 {
				resp.ByteStart = int32(offset)
				resp.HasAnchor = true
				line := 1
				for i := 0; i < offset && i < len(content); i++ {
					if content[i] == '\n' {
						line++
					}
				}
				resp.LineStart = int32(line)
				resp.LineEnd = int32(line)
				for i := offset; i < len(content) && int(resp.LineEnd) < line+3; i++ {
					if content[i] == '\n' {
						resp.LineEnd++
					}
				}
			}
		}

		return resp, nil
	}
	return nil, status.Error(codes.NotFound, "No such file in this snapshot.")
}
