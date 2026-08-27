package web

import (
	"context"
	"fmt"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"

	"github.com/nagiflow/nagipath/internal/probe"
	"github.com/nagiflow/nagipath/internal/store"
	"github.com/nagiflow/nagipath/internal/trace"
)

// ---------------------------------------------------------------- trace

type traceData struct {
	Query  trace.Query
	Method string
	Result *trace.Trace
	Empty  bool
	// Run is the live Probe this page is following, if the operator started one.
	Run *runView
	// Last is the probe most recently sent at this URL, read back with its evidence.
	// A probe has no screen of its own: it is evidence about one request, so it reads
	// beside that request and nowhere else.
	Last *probe.Past
	// Probes is how many probes have ever been sent at this URL, on the button that
	// leads to them — a button with no count is a promise that there is something
	// behind it.
	Probes int
	// URL is what goes back in the one input the form has: the canonical form of
	// whatever was asked for, however it arrived.
	URL string
	// Recent is every stored Trace, newest first, shown before anything has been
	// asked for on this visit — the trace was stored last time, so the operator
	// should not have to retype the URL to see it again.
	Recent []trace.Summary
	// Prov is the Provenance panel: the configuration lines one rule of this trace
	// was parsed from. The page used to link out to them and nothing more, so
	// checking a single rule cost a page load and the answer went off screen.
	Prov *provView
	// ProvPicked is whether Prov was asked for by a &prov= link, as opposed to
	// traceProv's own fallback to the first fired rule so the panel is never
	// empty. Only a real pick should force that rule's hop group open — the
	// fallback picking hop 0 every load is not a reason to start it expanded.
	ProvPicked bool
}

// provView is one rule's provenance with the file excerpt around it. Missing is
// set instead of the excerpt when there is none — a panel that silently shows
// nothing is worse than one that says why, and this is the screen an operator
// reads precisely because they do not believe the answer.
type provView struct {
	RuleID     int64
	Instance   string
	Object     string
	Rule       string
	Path       string
	Digest     string
	SnapshotID int64
	FileID     int64
	ByteStart  int
	Line       int
	Lines      []provLine
	Missing    string
}

type provLine struct {
	N    int
	Text string
	On   bool
}

// traceProv builds the Provenance panel for one rule of a trace. Which rule is a
// query parameter, not a click: the CSP allows no inline script, so each row's
// rule text is a link that selects itself. The default is the first rule that
// decided anything, so the panel is never empty on a trace that found a route.
func (s *Server) traceProv(ctx context.Context, t *trace.Trace, want int64) *provView {
	var hop *trace.Hop
	var rule *trace.Rule
	for _, h := range t.Hops {
		for _, hr := range fired(h.Rules) {
			if hr.Rule == nil {
				continue
			}
			if (want == 0 && rule == nil) || (want != 0 && hr.Rule.ID == want) {
				hop, rule = h, hr.Rule
			}
		}
	}
	if rule == nil {
		return nil
	}
	v := &provView{
		RuleID: rule.ID, Rule: strings.TrimSpace(rule.Directive + " " + rule.Args),
		Path: rule.Path, SnapshotID: rule.SnapshotID, FileID: rule.FileID,
		ByteStart: rule.ByteStart,
	}
	if hop.Inst != nil {
		v.Instance = instLabel(hop.Inst)
	}
	switch {
	case hop.Route != nil:
		v.Object = routeKind(hop.Route.MatchType) + " " + hop.Route.Pattern
	case hop.Site != nil:
		v.Object = siteKind(hop.Site.Kind) + " " + hop.Site.PrimaryName
	}
	if rule.FileID == 0 || rule.SnapshotID == 0 {
		v.Missing = "this rule was parsed before nagipath recorded file provenance, so there is no excerpt to show"
		return v
	}
	files, err := s.DB.SnapshotFiles(ctx, rule.SnapshotID)
	if err != nil {
		v.Missing = "the snapshot this rule came from could not be read back"
		return v
	}
	var ref *store.FileRef
	for i := range files {
		if files[i].ID == rule.FileID {
			ref = &files[i]
		}
	}
	if ref == nil {
		v.Missing = "the file this rule came from is no longer in that snapshot"
		return v
	}
	v.Digest, v.Path = ref.Digest, ref.Path
	body, err := s.DB.Blob(ctx, ref.Digest)
	if err != nil {
		v.Missing = "the stored copy of " + ref.Path + " could not be read"
		return v
	}
	// The excerpt is three lines of lead-in and four of follow-on, which is enough
	// to see the block a directive sits in without turning the panel into a file
	// viewer — "Open file" in the header is the file viewer.
	lines := strings.Split(string(body), "\n")
	at := strings.Count(string(body[:min(rule.ByteStart, len(body))]), "\n")
	v.Line = at + 1
	for i := max(0, at-3); i < min(len(lines), at+5); i++ {
		v.Lines = append(v.Lines, provLine{N: i + 1, Text: lines[i], On: i == at})
	}
	return v
}

// parseTarget turns one pasted URL into a trace query. The scheme is optional,
// because an operator copying out of a ticket has a hostname and a path far more
// often than a well-formed URL.
func parseTarget(raw string) (trace.Query, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return trace.Query{}, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return trace.Query{}, fmt.Errorf("%q is not a URL nagipath can read — try https://host/path", raw)
	}
	q := trace.Query{Scheme: u.Scheme, Hostname: u.Hostname(), Path: u.Path}
	if p, err := strconv.Atoi(u.Port()); err == nil {
		q.Port = p
	}
	return q, nil
}

// targetURL renders a query the way it would be pasted: the port only when it is
// not the scheme's default, because :443 on every https trace is noise.
func targetURL(q trace.Query) string {
	host := q.Hostname
	if q.Port != 0 && !(q.Scheme == "https" && q.Port == 443) && !(q.Scheme == "http" && q.Port == 80) {
		host = fmt.Sprintf("%s:%d", host, q.Port)
	}
	return q.Scheme + "://" + host + q.Path
}

func (s *Server) trace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := traceData{Method: "GET"}

	get := r.URL.Query().Get
	if r.Method == http.MethodPost {
		get = r.FormValue
	}
	if m := strings.ToUpper(strings.TrimSpace(get("method"))); m == "HEAD" {
		d.Method = m
	}
	// The form posts one pasted URL. The separate scheme/hostname/path/port
	// parameters still work unchanged, because every deep link in the app — the
	// certificate panel, drift, the recent table — builds a trace link that way.
	q, err := parseTarget(get("url"))
	if err != nil {
		redirect(w, r, "/trace", "", err.Error())
		return
	}
	if q.Hostname == "" {
		q = trace.Query{
			Scheme:   get("scheme"),
			Hostname: strings.TrimSpace(get("hostname")),
			Path:     strings.TrimSpace(get("path")),
		}
		if p, err := strconv.Atoi(get("port")); err == nil {
			q.Port = p
		}
	}
	if q.Hostname == "" {
		count, _ := s.DB.Instances(ctx)
		d.Empty = len(count) == 0
		d.Recent, _ = trace.Recent(ctx, s.DB, 20)
		s.render(w, r, "trace.html", "Trace a request", d)
		return
	}

	top, err := trace.Load(ctx, s.DB)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Result = trace.Walk(top, q)
	d.Query = d.Result.Query
	d.URL = targetURL(d.Query)
	d.Last, _ = probe.Last(ctx, s.DB, d.URL)
	d.Probes = probe.Count(ctx, s.DB, d.URL)
	runID, _ := strconv.ParseInt(get("run"), 10, 64)
	d.Run = s.probeRunView(runID)
	provID, _ := strconv.ParseInt(get("prov"), 10, 64)
	d.Prov = s.traceProv(ctx, d.Result, provID)
	d.ProvPicked = provID != 0

	// Only a POST stores the Trace. A shared link that recomputes on every view
	// would otherwise fill the table with duplicates.
	if r.Method == http.MethodPost {
		u := userOf(r)
		if id, err := trace.Save(ctx, s.DB, d.Result, nil); err != nil {
			s.Log.Warn("trace not stored", "err", err)
		} else {
			// The stored id is what a Probe attaches its evidence to.
			d.Result.ID = id
		}
		s.DB.Audit(ctx, &u.ID, "trace.compute", "trace", nil,
			q.Scheme+"://"+q.Hostname+q.Path)
	}
	s.render(w, r, "trace.html", "Trace "+q.Hostname+q.Path, d)
}

const probesPageSize = 50

// probeScreen displays one stored Probe with its full detail: request, audit,
// response stats, hop evidence, and matched log lines. Admin-only like POST.
// probeScreen is the form for running probes. GET with no probe= renders the
// empty form; GET with ?probe=<id> pre-fills from the stored probe and shows
// results below. POST /trace/probe already runs the probe.
func (s *Server) probeScreen(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	probeID, _ := strconv.ParseInt(r.URL.Query().Get("probe"), 10, 64)

	d := struct {
		Probe        *store.ProbeView
		Hops         []store.ProbeHopEvidence
		LogLines     []store.ProbeLogLine
		Gaps         []store.ProbeHopEvidence
		StateChanges int
	}{}

	// If a probe was requested, load it and its results
	if probeID > 0 {
		p, err := s.DB.LoadProbeView(ctx, probeID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if p == nil {
			redirect(w, r, "/trace/history", "", "probe not found")
			return
		}
		d.Probe = p

		hops, err := s.DB.ProbeHopEvidenceRows(ctx, probeID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.Hops = hops

		lines, err := s.DB.ProbeLogLines(ctx, probeID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.LogLines = lines

		gaps, err := s.DB.ProbeLogFormatGaps(ctx, probeID, p.TraceID.Int64)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		d.Gaps = gaps

		// Count state changes
		for _, h := range hops {
			if (h.Before == "inferred" || h.Before == "degraded") &&
				(h.After == "verified" || h.After == "observed_effect") {
				d.StateChanges++
			}
		}
	}

	s.render(w, r, "probe.html", "Probe", d)
}

// probeHistory is the audit record for every request nagipath sent on an operator's
// behalf. Reached from one trace it lists that entry point; reached with no url it
// lists the whole installation, which is the same screen and the same row shape.
func (s *Server) probeHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	d := struct {
		URL     string
		Query   string
		Range   string
		Actor   string
		Outcome string
		// Here is this screen with its filter, so selecting a row keeps the filter it
		// was selected from instead of resetting the page under the operator.
		Here       string
		Probes     []probe.Record
		Actors     []string
		Selected   *probe.Past
		Total      int
		ActorCount int
		From, To   int
		HasMore    bool
		NextCursor string
	}{
		URL:     q.Get("url"),
		Query:   strings.TrimSpace(q.Get("q")),
		Range:   q.Get("range"),
		Actor:   q.Get("actor"),
		Outcome: q.Get("outcome"),
	}

	// Every control in the filter bar reaches the query. A select an operator can
	// change that changes nothing on screen is worse than no select at all.
	days := 7
	switch d.Range {
	case "30":
		days = 30
	case "90":
		days = 90
	case "all":
		days = 0
	default:
		d.Range = "7"
	}
	switch d.Outcome {
	case "completed", "failed", "blocked", "running":
	default:
		d.Outcome = ""
	}
	d.Actors, _ = probe.Actors(ctx, s.DB)

	rows, err := probe.List(ctx, s.DB, probe.Filter{
		URL: d.URL, Q: d.Query, Actor: d.Actor, Outcome: d.Outcome, Days: days,
		// ponytail: one window's worth in memory, filtered and paged in Go, exactly
		// like the snapshots list. A probe is rate-limited per operator, so this is
		// thousands of rows at the very worst.
		Limit: 5000,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.Total = len(rows)
	seen := map[string]bool{}
	for _, p := range rows {
		if p.ActorLabel != "" && !seen[p.ActorLabel] {
			seen[p.ActorLabel] = true
			d.ActorCount++
		}
	}

	// The export is the filter, not the page: an operator who narrowed to one actor
	// and pressed Export audit means every probe that actor sent.
	if q.Get("export") == "csv" {
		out := [][]string{{"requested_at", "probe_id", "method", "entry_point", "status",
			"outcome", "error", "actor", "source", "evidence_rows", "state_changes"}}
		for _, p := range rows {
			status := ""
			if p.Status > 0 {
				status = strconv.Itoa(p.Status)
			}
			out = append(out, []string{p.RequestedAt, p.Token, p.Method, p.URL, status,
				p.Result, p.Err, p.ActorLabel, p.OriginHost, strconv.Itoa(p.Evidence),
				strings.Join(p.Changes, "; ")})
		}
		s.writeCSV(w, r, "probes", out)
		return
	}

	cursor, _ := strconv.Atoi(q.Get("cursor"))
	if cursor < 0 || cursor > len(rows) {
		cursor = 0
	}
	end := min(cursor+probesPageSize, len(rows))
	if end < len(rows) {
		d.HasMore = true
		d.NextCursor = strconv.Itoa(end)
	}
	d.Probes = rows[cursor:end]
	if len(d.Probes) > 0 {
		d.From, d.To = cursor+1, end
	}

	// A row selects itself by probe id. The default is the newest row the filter
	// matched, so the detail panel is never empty next to a table that has rows.
	if id, err := strconv.ParseInt(q.Get("probe"), 10, 64); err == nil && id > 0 {
		d.Selected, _ = probe.Load(ctx, s.DB, id)
	} else if len(d.Probes) > 0 {
		d.Selected, _ = probe.Load(ctx, s.DB, d.Probes[0].ProbeID)
	}
	d.Here = "/trace/history?url=" + neturl.QueryEscape(d.URL) +
		"&q=" + neturl.QueryEscape(d.Query) + "&range=" + d.Range +
		"&actor=" + neturl.QueryEscape(d.Actor) + "&outcome=" + d.Outcome

	s.render(w, r, "probe_history.html", "Probe history", d)
}
