package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// getSnapshots and getSnapshotFile moved to snapshotservice.go as
// SnapshotService's ListSnapshots and GetSnapshotFile RPCs (docs/adr/0018,
// proto/nagipath/api/v1/snapshots.proto). snapshotRow, rangeSince,
// snapshotsPageSize and snapshotRowsFrom stay here: shared with
// snapshotservice.go and getSnapshotsCSV.

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

// snapshotRowsFrom builds the working row list and tallies resp.Total/
// TotalBytes/Changed/Degraded in the same pass — one snapshot's state feeds
// both a row and a running total, unchanged from the pre-gateway handler.
func snapshotRowsFrom(all []store.SnapshotRow, resp *pb.SnapshotsListResponse) []snapshotRow {
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
	return rows
}

// getSnapshotsCSV: GET /snapshots?export=csv is a formatted download, not
// RPC-shaped data (gateway.go's gatewayOrCSV, wired in api.go).
func (s *Server) getSnapshotsCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	rng := q.Get("range")
	switch rng {
	case "7d", "30d", "90d":
	default:
		rng = "24h"
	}

	all, err := s.DB.SnapshotsSince(ctx, rangeSince(rng))
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.SnapshotsListResponse{}
	rows := snapshotRowsFrom(all, resp)

	if query := strings.TrimSpace(q.Get("q")); query != "" {
		qlow := strings.ToLower(query)
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
	switch q.Get("changes") {
	case "yes", "no":
		want := q.Get("changes") == "yes"
		var kept []snapshotRow
		for _, row := range rows {
			if row.Changed == want {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if trigger := q.Get("trigger"); trigger != "" {
		var kept []snapshotRow
		for _, row := range rows {
			if row.Trigger == trigger {
				kept = append(kept, row)
			}
		}
		rows = kept
	}

	out := [][]string{{"captured_at", "instance", "node", "cluster", "trigger",
		"changed", "bytes_raw", "state"}}
	for _, row := range rows {
		out = append(out, []string{row.CapturedAt, row.Instance, row.Node, row.Cluster,
			row.Trigger, strconv.FormatBool(row.Changed),
			strconv.FormatInt(row.BytesRaw, 10), row.State})
	}
	s.writeCSV(w, r, "snapshots", out)
}
