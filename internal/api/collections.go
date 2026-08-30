package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// getCollections moved to collectionservice.go as CollectionService's
// ListCollections RPC (docs/adr/0018, proto/nagipath/api/v1/settings.proto).
// Everything below stays here: shared with collectionservice.go and
// getCollectionsCSV.

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

type collectionRow struct {
	id, nodeID                   int64
	nodeName, trigger, startedAt string
	status, errStr               string
	instancesSeen                int
	durationMS                   int64
}

func collectionRowsFrom(all []store.Collection) []collectionRow {
	var rows []collectionRow
	for _, c := range all {
		dur := int64(0)
		if c.DurationMS.Valid {
			dur = c.DurationMS.Int64
		}
		rows = append(rows, collectionRow{id: c.ID, nodeID: c.NodeID, nodeName: c.NodeName, trigger: c.Trigger,
			startedAt: c.StartedAt, status: c.Status, errStr: c.Error, instancesSeen: c.InstancesSeen, durationMS: dur})
	}
	return rows
}

// getCollectionsCSV: GET /collections?export=csv is a formatted download,
// not RPC-shaped data (gateway.go's gatewayOrCSV, wired in api.go).
func (s *Server) getCollectionsCSV(w http.ResponseWriter, r *http.Request) {
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

	all, err := s.DB.CollectionsSince(ctx, collectionSince(rng))
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
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
	if status != "" {
		var kept []collectionRow
		for _, rr := range rows {
			if rr.status == status {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}
	if trigger != "" {
		var kept []collectionRow
		for _, rr := range rows {
			if rr.trigger == trigger {
				kept = append(kept, rr)
			}
		}
		rows = kept
	}

	out := [][]string{{"started_at", "node", "trigger", "duration_ms", "outcome", "status"}}
	for _, rr := range rows {
		out = append(out, []string{rr.startedAt, rr.nodeName, rr.trigger,
			strconv.FormatInt(rr.durationMS, 10), collectionOutcome(rr.errStr, rr.instancesSeen), rr.status})
	}
	s.writeCSV(w, r, "collections", out)
}
