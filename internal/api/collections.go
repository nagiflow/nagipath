package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
)

const collectionsPageSize = 50

// collectionSince ports internal/web/collections.go's collectionSince: turns
// the range select into the earliest timestamp to load.
func collectionSince(r string) string {
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

func collectionOutcome(errStr string, instancesSeen int) string {
	if errStr != "" {
		return errStr
	}
	if instancesSeen == 0 {
		return "no change"
	}
	if instancesSeen == 1 {
		return "1 change"
	}
	return strconv.Itoa(instancesSeen) + " changes"
}

// getCollections ports internal/web/collections.go's collections(): job
// history, scoped in SQL to the selected window, then filtered and
// paginated in memory (docs/adr/0018).
func (s *Server) getCollections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	node := strings.TrimSpace(q.Get("node"))
	status := q.Get("status")
	trigger := q.Get("trigger")
	rng := q.Get("range")
	switch rng {
	case "7d", "30d", "90d":
	default:
		rng = "24h"
	}

	since := collectionSince(rng)
	all, err := s.DB.CollectionsSince(ctx, since)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	nodes, err := s.DB.Nodes(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.CollectionsResponse{Empty: len(nodes) == 0}
	if resp.Empty {
		writeProto(w, http.StatusOK, resp)
		return
	}

	type row struct {
		id, nodeID                   int64
		nodeName, trigger, startedAt string
		status, errStr               string
		instancesSeen                int
		durationMS                   int64
	}
	var rows []row
	for _, c := range all {
		dur := int64(0)
		if c.DurationMS.Valid {
			dur = c.DurationMS.Int64
		}
		rows = append(rows, row{id: c.ID, nodeID: c.NodeID, nodeName: c.NodeName, trigger: c.Trigger,
			startedAt: c.StartedAt, status: c.Status, errStr: c.Error, instancesSeen: c.InstancesSeen, durationMS: dur})
	}

	if node != "" {
		nodelow := strings.ToLower(node)
		var kept []row
		for _, rr := range rows {
			if strings.Contains(strings.ToLower(rr.nodeName), nodelow) {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}
	if status != "" {
		var kept []row
		for _, rr := range rows {
			if rr.status == status {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}
	if trigger != "" {
		var kept []row
		for _, rr := range rows {
			if rr.trigger == trigger {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}

	stats, err := s.DB.CollectionStatsFor(ctx, since, node, status, trigger)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp.Total, resp.Succeeded, resp.Degraded = int32(stats.Runs), int32(stats.Succeeded), int32(stats.Degraded)
	resp.Failed, resp.Running = int32(stats.Failed), int32(stats.Running)
	resp.MedianMs, resp.P95Ms = stats.MedianMS, stats.P95MS

	if q.Get("export") == "csv" {
		out := [][]string{{"started_at", "node", "trigger", "duration_ms", "outcome", "status"}}
		for _, rr := range rows {
			out = append(out, []string{rr.startedAt, rr.nodeName, rr.trigger,
				strconv.FormatInt(rr.durationMS, 10), collectionOutcome(rr.errStr, rr.instancesSeen), rr.status})
		}
		s.writeCSV(w, r, "collections", out)
		return
	}

	cursor, _ := strconv.Atoi(q.Get("cursor"))
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
	writeProto(w, http.StatusOK, resp)
}
