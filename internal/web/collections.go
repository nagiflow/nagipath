package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type collectionRow struct {
	ID            int64
	NodeID        int64
	NodeName      string
	Trigger       string
	StartedAt     string
	Status        string
	Error         string
	InstancesSeen int
	DurationMS    int64
}

type collectionsData struct {
	List       []collectionRow
	Node       string
	Status     string
	Trigger    string
	Range      string
	Total      int
	Succeeded  int
	Degraded   int
	Failed     int
	Running    int // queue figure
	MedianMS   int64
	P95MS      int64
	NextCursor string
	HasMore    bool
	// From and To are the 1-based range this page shows, because "50 of 200" printed
	// on page two claims to be page one.
	From, To int
	Empty    bool
}

const collectionsPageSize = 50

// collectionSince turns the range select into the earliest timestamp to load.
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

func (s *Server) collections(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	d := collectionsData{
		Node:    strings.TrimSpace(q.Get("node")),
		Status:  q.Get("status"),
		Trigger: q.Get("trigger"),
		Range:   q.Get("range"),
	}
	switch d.Range {
	case "7d", "30d", "90d":
	default:
		d.Range = "24h"
	}

	// Scoped in SQL to the window, not the newest 10,000 rows: a fleet collecting
	// daily reaches that cap inside a 90-day window and the tail vanishes with no
	// sign on the page that it did.
	since := collectionSince(d.Range)
	all, err := s.DB.CollectionsSince(ctx, since)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Empty means "no collections ever", not "none in this window".
	nodes, err := s.DB.Nodes(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Empty = len(nodes) == 0
	if d.Empty {
		s.render(w, r, "collections.html", "Collection jobs", d)
		return
	}

	var rows []collectionRow
	for _, c := range all {
		dur := int64(0)
		if c.DurationMS.Valid {
			dur = c.DurationMS.Int64
		}
		rows = append(rows, collectionRow{
			ID: c.ID, NodeID: c.NodeID, NodeName: c.NodeName, Trigger: c.Trigger,
			StartedAt: c.StartedAt, Status: c.Status, Error: c.Error,
			InstancesSeen: c.InstancesSeen, DurationMS: dur,
		})
	}

	// The filters are AND'd: every active filter narrows the set.
	if d.Node != "" {
		nodelow := strings.ToLower(d.Node)
		var kept []collectionRow
		for _, row := range rows {
			if strings.Contains(strings.ToLower(row.NodeName), nodelow) {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if d.Status != "" {
		var kept []collectionRow
		for _, row := range rows {
			if row.Status == d.Status {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if d.Trigger != "" {
		var kept []collectionRow
		for _, row := range rows {
			if row.Trigger == d.Trigger {
				kept = append(kept, row)
			}
		}
		rows = kept
	}

	// Stats are over the filtered set: an operator who narrowed to one node wants
	// that node's stats, not the fleet's. Use the new aggregator that also computes
	// median and queue count.
	stats, err := s.DB.CollectionStatsFor(ctx, since, d.Node, d.Status, d.Trigger)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Total = stats.Runs
	d.Succeeded = stats.Succeeded
	d.Degraded = stats.Degraded
	d.Failed = stats.Failed
	d.Running = stats.Running
	d.MedianMS = stats.MedianMS
	d.P95MS = stats.P95MS

	// The export is the filtered set, not the page.
	if q.Get("export") == "csv" {
		out := [][]string{{"started_at", "node", "trigger", "duration_ms", "outcome", "status"}}
		for _, row := range rows {
			outcome := "snapshot stored"
			if row.Error != "" {
				outcome = row.Error
			} else if row.InstancesSeen > 0 {
				outcome = outcome + ", " + strconv.Itoa(row.InstancesSeen) + " instance(s)"
			}
			out = append(out, []string{row.StartedAt, row.NodeName, row.Trigger,
				strconv.FormatInt(row.DurationMS, 10), outcome, row.Status})
		}
		s.writeCSV(w, r, "collections", out)
		return
	}

	// Cursor pagination: "Load more".
	cursor, _ := strconv.Atoi(q.Get("cursor"))
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	end := cursor + collectionsPageSize
	if end > len(rows) {
		end = len(rows)
	} else {
		d.HasMore = true
		d.NextCursor = strconv.Itoa(end)
	}
	d.List = rows[cursor:end]
	if len(d.List) > 0 {
		d.From, d.To = cursor+1, end
	}

	s.render(w, r, "collections.html", "Collection jobs", d)
}

// collectionOutcome returns the prose outcome for display: what happened, not
// just the state. This is the split the wireframe draws between Outcome and State.
func collectionOutcome(row collectionRow) string {
	if row.Error != "" {
		return row.Error
	}
	if row.InstancesSeen == 0 {
		return "no change"
	}
	if row.InstancesSeen == 1 {
		return "1 change"
	}
	return strconv.Itoa(row.InstancesSeen) + " changes"
}
