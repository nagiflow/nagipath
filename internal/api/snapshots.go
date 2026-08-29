package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
)

type snapshotRow struct {
	ID         int64
	InstanceID int64
	Instance   string
	Node       string
	Cluster    string
	CapturedAt string
	Trigger    string
	Changed    bool
	BytesRaw   int64
	State      string
}

const snapshotsPageSize = 50

// rangeSince is internal/web/snapshots.go's helper, unchanged.
func rangeSince(r string) string {
	days := 1
	switch r {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	}
	return time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
}

// getSnapshots ports internal/web/snapshots.go's snapshots(): same range/
// query/changes/trigger filters, same cursor pagination, same CSV export.
func (s *Server) getSnapshots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	resp := &pb.SnapshotsListResponse{
		Query: strings.TrimSpace(q.Get("q")), Range: q.Get("range"),
		Changes: q.Get("changes"), Trigger: q.Get("trigger"),
	}
	switch resp.Range {
	case "7d", "30d", "90d":
	default:
		resp.Range = "24h"
	}
	resp.RetentionDays = int32(s.DB.SettingInt(ctx, "snapshot_retention_days"))

	all, err := s.DB.SnapshotsSince(ctx, rangeSince(resp.Range))
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp.Empty = len(instances) == 0
	if resp.Empty {
		writeProto(w, http.StatusOK, resp)
		return
	}

	var rows []snapshotRow
	for _, sn := range all {
		state := "ok"
		switch {
		case sn.Degraded:
			state = "degraded"
			resp.Degraded++
		case sn.ParseState != "parsed" && sn.ParseState != "":
			state = "failed"
		}
		if sn.Changed {
			resp.Changed++
		}
		rows = append(rows, snapshotRow{
			ID: sn.ID, InstanceID: sn.InstanceID, Instance: sn.Instance,
			Node: sn.Node, Cluster: sn.Cluster, CapturedAt: sn.CapturedAt, Trigger: sn.Trigger,
			Changed: sn.Changed, BytesRaw: sn.BytesRaw, State: state,
		})
		resp.Total++
		resp.TotalBytes += sn.BytesRaw
	}

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

	if q.Get("export") == "csv" {
		out := [][]string{{"captured_at", "instance", "node", "cluster", "trigger",
			"changed", "bytes_raw", "state"}}
		for _, row := range rows {
			out = append(out, []string{row.CapturedAt, row.Instance, row.Node, row.Cluster,
				row.Trigger, strconv.FormatBool(row.Changed),
				strconv.FormatInt(row.BytesRaw, 10), row.State})
		}
		s.writeCSV(w, r, "snapshots", out)
		return
	}

	cursor, _ := strconv.Atoi(q.Get("cursor"))
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

	writeProto(w, http.StatusOK, resp)
}

// getSnapshotFile ports internal/web/snapshots.go's snapshotFile().
func (s *Server) getSnapshotFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	snapID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	fileID, _ := strconv.ParseInt(r.PathValue("fileID"), 10, 64)

	snap, err := s.DB.SnapshotByID(ctx, snapID)
	if err != nil {
		apiError(w, http.StatusNotFound, "not_found", "No such snapshot.")
		return
	}
	files, err := s.DB.SnapshotFiles(ctx, snap.ID)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	for _, f := range files {
		if f.ID != fileID {
			continue
		}
		body, err := s.DB.Blob(ctx, f.Digest)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
			return
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

		anchor := r.URL.Query().Get("b")
		if anchor == "" {
			anchor = strings.TrimPrefix(r.URL.Fragment, "b")
		}
		if anchor != "" {
			if offset, err := strconv.Atoi(anchor); err == nil && offset >= 0 {
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

		writeProto(w, http.StatusOK, resp)
		return
	}
	apiError(w, http.StatusNotFound, "not_found", "No such file in this snapshot.")
}
