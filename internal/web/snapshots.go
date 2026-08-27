package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// snapshotRow is one row in the snapshots list: one immutable capture, with its
// instance, cluster and collection metadata.
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
	State      string // confidence badge: ok, degraded, failed
}

type snapshotsData struct {
	List       []snapshotRow
	Query      string
	Range      string
	Changes    string
	Trigger    string
	Total      int
	TotalBytes int64
	Filtered   int
	Changed    int
	Degraded   int
	NextCursor string
	HasMore    bool
	// From and To are the 1-based range this page shows. "50 of 200" was printed on
	// every page, so page two claimed to be page one.
	From, To int
	Empty    bool
	// The configured retention window, so the page states the fleet's own number
	// rather than the default it was written against.
	RetentionDays int
}

const snapshotsPageSize = 50

// rangeSince turns the range select into the earliest timestamp to load. The
// filter used to be read, printed back into the select, and then never applied —
// "Range: 24h" listed captures from every day nagipath had ever run.
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

func (s *Server) snapshots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	d := snapshotsData{
		Query:   strings.TrimSpace(q.Get("q")),
		Range:   q.Get("range"),
		Changes: q.Get("changes"),
		Trigger: q.Get("trigger"),
	}
	switch d.Range {
	case "7d", "30d", "90d":
	default:
		d.Range = "24h"
	}
	d.RetentionDays = s.DB.SettingInt(ctx, "snapshot_retention_days")

	all, err := s.DB.SnapshotsSince(ctx, rangeSince(d.Range))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	// Empty means "nothing has ever been collected", not "nothing in this window":
	// the two need different words, and the range select is useless on a page that
	// tells the operator to go and connect the fleet.
	instances, err := s.DB.Instances(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Empty = len(instances) == 0
	if d.Empty {
		s.render(w, r, "snapshots.html", "Snapshots", d)
		return
	}

	var rows []snapshotRow
	for _, sn := range all {
		// The whole word, not "deg"/"err": the badge is the word, and an
		// abbreviation is one more thing an operator has to learn.
		state := "ok"
		switch {
		case sn.Degraded:
			state = "degraded"
			d.Degraded++
		case sn.ParseState != "parsed" && sn.ParseState != "":
			state = "failed"
		}
		if sn.Changed {
			d.Changed++
		}
		rows = append(rows, snapshotRow{
			ID: sn.ID, InstanceID: sn.InstanceID, Instance: sn.Instance,
			Node: sn.Node, Cluster: sn.Cluster, CapturedAt: sn.CapturedAt, Trigger: sn.Trigger,
			Changed: sn.Changed, BytesRaw: sn.BytesRaw, State: state,
		})
		d.Total++
		d.TotalBytes += sn.BytesRaw
	}

	// ponytail: filtering in Go over the window's rows. Push into SQL when a
	// 90-day window stops fitting in memory comfortably.
	if d.Query != "" {
		qlow := strings.ToLower(d.Query)
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
	switch d.Changes {
	case "yes", "no":
		want := d.Changes == "yes"
		var kept []snapshotRow
		for _, row := range rows {
			if row.Changed == want {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	if d.Trigger != "" {
		var kept []snapshotRow
		for _, row := range rows {
			if row.Trigger == d.Trigger {
				kept = append(kept, row)
			}
		}
		rows = kept
	}

	d.Filtered = len(rows)

	// The export is the filtered set, not the page: an operator who narrowed to
	// three clusters and pressed Export means those three clusters.
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

	// Cursor pagination: "Load more"
	cursor, _ := strconv.Atoi(q.Get("cursor"))
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	end := cursor + snapshotsPageSize
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

	s.render(w, r, "snapshots.html", "Snapshots", d)
}

func (s *Server) snapshotFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	snap, err := s.DB.SnapshotByID(ctx, idOf(r, "id"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	files, err := s.DB.SnapshotFiles(ctx, snap.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	wanted := idOf(r, "fileID")
	for _, f := range files {
		if f.ID != wanted {
			continue
		}
		body, err := s.DB.Blob(ctx, f.Digest)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d := fileData{Snapshot: snap, File: f, Body: string(body)}
		d.Instance, _ = s.DB.Instance(ctx, snap.InstanceID)
		// `?b=123` is the byte offset to highlight; `#b123` is the same offset as a
		// fragment, which scrolls to it. Both, because a browser never sends the
		// fragment to the server — reading r.URL.Fragment here meant the highlight
		// could only ever fire for a request nothing makes, so a provenance link
		// landed in the middle of a config file with nothing marked.
		anchor := r.URL.Query().Get("b")
		if anchor == "" {
			anchor = strings.TrimPrefix(r.URL.Fragment, "b")
		}
		if anchor != "" {
			if offset, err := strconv.Atoi(anchor); err == nil && offset >= 0 {
				d.ByteStart = offset
				d.HasAnchor = true
				// Convert byte offset to line number for highlighting
				line := 1
				for i := 0; i < offset && i < len(d.Body); i++ {
					if d.Body[i] == '\n' {
						line++
					}
				}
				d.LineStart = line
				// Highlight a few lines around the target for context
				d.LineEnd = line
				for i := offset; i < len(d.Body) && d.LineEnd < line+3; i++ {
					if d.Body[i] == '\n' {
						d.LineEnd++
					}
				}
			}
		}
		s.render(w, r, "file.html", f.Path, d)
		return
	}
	s.notFound(w, r)
}

type certData struct {
	List []store.CertificateView
	// Sel is the certificate the right-hand panel is describing. Selection is a
	// query parameter rather than script, so a link can point at one certificate.
	Sel      *store.CertificateView
	Bindings []store.CertBinding
	// Trace is the entry point the panel's "trace affected entry points" link
	// prefills, derived from the first name the certificate actually serves.
	Trace string
}
