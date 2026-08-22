// Package probe sends exactly one request, when an operator asks for it, and
// turns what comes back into evidence.
//
// The rules that make this safe are structural, not conventions:
//
//   - GET and HEAD only. The database enforces it with a CHECK; this package
//     refuses before it gets that far.
//   - Operator-triggered only. There is no scheduler, no retry loop and no
//     background caller anywhere in the product, so a Probe cannot become traffic.
//   - Every Probe is audited with the actor, the target and the host the request
//     went out from, because from the target's side this is indistinguishable from
//     any other client and the only record of who did it lives here.
//
// Probe is also the only thing in nagipath that can produce `verified`. A response
// header proves an effect was observed; only an access-log line carrying the
// correlation token proves which Instance handled the request. Where the log format
// cannot prove it, the gap is stated and the hop stays Inferred: an honest Inferred
// beats a confident wrong Verified (probe.md §6).
package probe

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/collect"
	"github.com/nagiflow/nagipath/internal/sshx"
	"github.com/nagiflow/nagipath/internal/store"
)

// logTailBytes bounds the access log read.
//
// ponytail: nagipath tails a fixed window and matches the token in Go rather than
// grepping on the target, because a piped `grep` cannot be granted narrowly in
// sudoers and `sudo sh -c` is a root shell wearing a narrow-grant label. The
// ceiling is real: on a log doing more than this between the request and the read,
// the line falls off the window and the hop stays Inferred, which is the correct
// failure direction. Upgrade path is a token-bounded reader command in internal/sshx.
const logTailBytes = 512 << 10

// Prober runs Probes. Client and Dialer are seams so this can be tested without a
// fleet or a network.
type Prober struct {
	DB      *store.DB
	Dialer  collect.Connector
	Log     *slog.Logger
	Client  *http.Client
	Version string
}

type Options struct {
	TraceID int64
	// URL is the entry point as the operator typed it on the Trace screen.
	URL            string
	Method         string
	MaxRedirects   int
	SendQueryToken bool
	ActorID        int64
	OriginHost     string
	// OnStep is called as the Probe works, so a screen can follow along instead of
	// waiting on a spinner for however long a fleet of SSH log reads takes. It is
	// called from Run's goroutine and must not block for long.
	OnStep func(Step)
}

// Step is one thing the Probe did, emitted while it happens. State is what the hop
// should look like on the path graph — "checking", "verified" or "missing" — and is
// empty for a step that is not about one hop.
type Step struct {
	Text     string
	Ordinal  int
	Instance string
	State    string
}

// Hop states a Step can carry. They are strings because they are also CSS classes.
const (
	StateChecking = "checking"
	StateVerified = "verified"
	StateMissing  = "missing"
)

// Stage is one of the three things a Probe can learn, in the order it learns them.
type Stage struct {
	Title string
	Lines []string
	// Badge is the confidence this stage can grant, or "" when it grants nothing.
	Badge string
}

// HopOutcome is the before/after a Probe produced for one hop. Before and After
// are deliberately both shown: a hop that did not move is the interesting case.
type HopOutcome struct {
	Ordinal  int
	Instance string
	Vendor   string
	Evidence string
	Before   string
	After    string
	Note     string
}

// Gap is a vendor log format that cannot prove what the Probe needs. nagipath
// states the directive that would fix it and never applies it.
type Gap struct {
	Instance  string
	Vendor    string
	Note      string
	Directive string
}

type Result struct {
	ProbeID       int64
	Token         string
	Method        string
	URL           string
	Status        int
	StatusText    string
	Headers       [][2]string
	Redirects     []string
	DurationMS    int64
	Stages        []Stage
	Hops          []HopOutcome
	Gaps          []Gap
	Err           string
	RequestedAt   string
	ActorLabel    string
	OriginHost    string
	StillInferred []string
}

type hopRow struct {
	ID         int64
	Ordinal    int
	External   bool
	InstanceID int64
	Instance   string
	Vendor     string
	NodeID     int64
	// Node is the host the Instance runs on. The live action log names hosts because
	// that is how an operator names them: "nginx nginx.conf" on three hosts is not a
	// name, and this is the one place a step has to be read at a glance.
	Node       string
	LogPaths   []string
	Confidence string
}

func (h hopRow) label() string {
	if h.Node != "" {
		return h.Node
	}
	return h.Instance
}

// Run performs the Probe. It always stores a probe row, including on failure: a
// request that went out and is not recorded is the one thing an audit trail cannot
// tolerate.
func (p *Prober) Run(ctx context.Context, o Options) (*Result, error) {
	method := strings.ToUpper(strings.TrimSpace(o.Method))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "HEAD" {
		return nil, fmt.Errorf("a probe is GET or HEAD only; %q would change something", method)
	}
	target, err := url.Parse(strings.TrimSpace(o.URL))
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("%q is not a URL nagipath can probe", o.URL)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return nil, fmt.Errorf("a probe speaks http or https, not %q", target.Scheme)
	}
	maxRedirects := o.MaxRedirects
	if maxRedirects < 0 || maxRedirects > 20 {
		maxRedirects = p.DB.SettingInt(ctx, "probe_max_redirects")
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}
	res := &Result{
		Token: token, Method: method, URL: target.String(),
		RequestedAt: store.Now(), OriginHost: o.OriginHost,
	}

	probeID, err := p.insertProbe(ctx, o, method, target.String(), token)
	if err != nil {
		return nil, err
	}
	res.ProbeID = probeID

	// The audit entry is written before the request leaves, so a probe that hangs
	// or crashes the process still has a record of who sent it and where.
	p.DB.AuditDetail(ctx, &o.ActorID, "probe.run", "probe", &probeID, target.String(),
		map[string]any{"method": method, "correlation_token": token,
			"max_redirects": maxRedirects, "origin_host": o.OriginHost},
		"success", o.OriginHost)

	emit := o.OnStep
	if emit == nil {
		emit = func(Step) {}
	}

	hops, err := p.hops(ctx, o.TraceID)
	if err != nil {
		p.Log.Warn("probe: trace hops unreadable", "trace", o.TraceID, "err", err)
	}

	emit(Step{Ordinal: -1, Text: method + " " + target.String() + " · correlation token " + token[:8]})
	stage1, err := p.request(ctx, res, target, method, token, maxRedirects, o.SendQueryToken)
	res.Stages = append(res.Stages, stage1)
	if err != nil {
		res.Err = err.Error()
		emit(Step{Ordinal: -1, Text: "the request failed: " + err.Error()})
		p.finishProbe(ctx, probeID, res, "failed")
		return res, nil
	}
	emit(Step{Ordinal: -1, Text: fmt.Sprintf("responded %d in %dms", res.Status, res.DurationMS)})

	res.Stages = append(res.Stages, p.observedEffect(ctx, probeID, hops, res, emit))
	res.Stages = append(res.Stages, p.correlate(ctx, probeID, hops, res, emit))
	p.finishProbe(ctx, probeID, res, "completed")

	// The before/after table is the point of the screen: it says what this one
	// request actually changed about what nagipath claims to know.
	for _, h := range hops {
		if h.External || h.InstanceID == 0 {
			continue
		}
		after := p.hopConfidence(ctx, h.ID)
		out := HopOutcome{Ordinal: h.Ordinal, Instance: h.Instance, Vendor: h.Vendor,
			Before: h.Confidence, After: after}
		switch {
		case after == "verified":
			out.Evidence = "access log line carrying the correlation token"
		case after == "observed_effect":
			out.Evidence = "response header this hop declares"
		default:
			out.Evidence = "log lacks a required field"
			out.Note = "this hop stayed Inferred"
			res.StillInferred = append(res.StillInferred, h.Instance)
		}
		res.Hops = append(res.Hops, out)
	}
	p.refreshTraceConfidence(ctx, o.TraceID)
	emit(Step{Ordinal: -1, Text: fmt.Sprintf("done · %d hop(s) verified from their own access log",
		verifiedCount(res))})
	return res, nil
}

func verifiedCount(res *Result) int {
	n := 0
	for _, h := range res.Hops {
		if h.After == "verified" {
			n++
		}
	}
	return n
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ------------------------------------------------------------------- stage one

func (p *Prober) request(ctx context.Context, res *Result, target *url.URL,
	method, token string, maxRedirects int, queryToken bool) (Stage, error) {

	st := Stage{Title: "1 · response"}
	client := p.Client
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
			// Redirects are followed by hand so the chain can be recorded and the
			// cap enforced as a cap, not as an error after the fact.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	ua := fmt.Sprintf("nagipath-probe/%s (+correlation:%s)", p.version(), token)

	current := *target
	start := time.Now()
	for hop := 0; ; hop++ {
		u := current
		if queryToken {
			q := u.Query()
			q.Set("__nagipath", token)
			u.RawQuery = q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
		if err != nil {
			return st, err
		}
		req.Header.Set("X-Nagipath-Probe", token)
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Accept", "*/*")

		resp, err := client.Do(req)
		if err != nil {
			return st, err
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		resp.Body.Close()

		res.Status = resp.StatusCode
		res.StatusText = resp.Status
		res.Headers = flattenHeaders(resp.Header)

		loc := resp.Header.Get("Location")
		if resp.StatusCode < 300 || resp.StatusCode > 399 || loc == "" {
			break
		}
		next, err := current.Parse(loc)
		if err != nil {
			res.Redirects = append(res.Redirects, resp.Status+" → "+loc+" (unparseable)")
			break
		}
		res.Redirects = append(res.Redirects, fmt.Sprintf("%d → %s", resp.StatusCode, next.String()))
		if hop+1 >= maxRedirects {
			st.Lines = append(st.Lines,
				fmt.Sprintf("redirect cap of %d reached; nagipath stopped following", maxRedirects))
			break
		}
		current = *next
	}
	res.DurationMS = time.Since(start).Milliseconds()

	st.Lines = append(st.Lines, fmt.Sprintf("%d · %dms", res.Status, res.DurationMS))
	for _, h := range res.Headers {
		if isInterestingHeader(h[0]) {
			st.Lines = append(st.Lines, h[0]+": "+h[1])
		}
	}
	if len(res.Redirects) > 0 {
		st.Lines = append(st.Lines, fmt.Sprintf("%d redirect(s) followed", len(res.Redirects)))
	}
	return st, nil
}

// isInterestingHeader keeps the response summary to the headers a configuration
// decides. Date and Content-Length are noise here.
func isInterestingHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Date", "Content-Length", "Connection", "Accept-Ranges", "Etag", "Last-Modified":
		return false
	}
	return true
}

func flattenHeaders(h http.Header) [][2]string {
	var out [][2]string
	for name, vals := range h {
		for _, v := range vals {
			out = append(out, [2]string{name, v})
		}
	}
	sortPairs(out)
	return out
}

func sortPairs(p [][2]string) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && p[j-1][0] > p[j][0]; j-- {
			p[j-1], p[j] = p[j], p[j-1]
		}
	}
}

// ------------------------------------------------------------------- stage two

// observedEffect matches the response against the header rules each hop declares.
// A header that is declared and absent is the strongest thing a Probe produces: it
// is proof the configuration an operator is reading is not the configuration that
// answered, and it is recorded as `disproved` rather than as silence.
func (p *Prober) observedEffect(ctx context.Context, probeID int64, hops []hopRow, res *Result,
	emit func(Step)) Stage {
	st := Stage{Title: "2 · observed effect"}
	have := map[string]string{}
	for _, h := range res.Headers {
		have[strings.ToLower(h[0])] = h[1]
	}

	confirmed, absent := 0, 0
	for _, h := range hops {
		if h.External || h.InstanceID == 0 {
			continue
		}
		rules, err := p.headerRules(ctx, h.ID)
		if err != nil {
			continue
		}
		// Per hop, because the raise below is about this hop: a header confirmed on the
		// balancer says nothing about the backend behind it.
		hopConfirmed := 0
		if len(rules) > 0 {
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateChecking,
				Text: h.label() + ": checking the response for the headers it declares"})
		}
		for _, r := range rules {
			name, want := headerFromArgs(r.directive, r.args)
			if name == "" {
				continue
			}
			got, present := have[strings.ToLower(name)]
			switch {
			case present && (want == "" || strings.Contains(got, want)):
				p.addEvidence(ctx, probeID, h, r.id, "response_header",
					name+": "+got, map[string]any{"header": name, "declared_by": r.directive},
					"observed_effect")
				p.setHopRuleConfidence(ctx, h.ID, r.id, "observed_effect")
				confirmed++
				hopConfirmed++
				st.Lines = append(st.Lines, name+" present · "+h.Instance)
			case r.shadowed:
				// Already known to be shadowed; the absence is the confirmation.
				p.addEvidence(ctx, probeID, h, r.id, "header_absent",
					name+" not in the response",
					map[string]any{"header": name, "declared_by": r.directive,
						"note": "declared but shadowed by a nearer scope"}, "disproved")
				absent++
				st.Lines = append(st.Lines, name+" absent · shadowed as predicted")
			default:
				p.addEvidence(ctx, probeID, h, r.id, "header_absent",
					name+" not in the response",
					map[string]any{"header": name, "declared_by": r.directive}, "disproved")
				absent++
				st.Lines = append(st.Lines, name+" declared but absent · "+h.Instance)
			}
		}
		if hopConfirmed > 0 {
			p.raiseHop(ctx, h.ID, "observed_effect")
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateChecking,
				Text: h.label() + ": a header it declares is in the response"})
		}
	}
	if len(st.Lines) == 0 {
		st.Lines = append(st.Lines, "no hop on this trace declares a response header to check")
	}
	if confirmed > 0 {
		st.Badge = "observed_effect"
	}
	if absent > 0 {
		st.Lines = append(st.Lines,
			fmt.Sprintf("%d declared header(s) did not arrive", absent))
	}
	return st
}

type ruleRow struct {
	id        int64
	directive string
	args      string
	shadowed  bool
}

func (p *Prober) headerRules(ctx context.Context, hopID int64) ([]ruleRow, error) {
	rows, err := p.DB.R.QueryContext(ctx, `SELECT r.id, r.directive, r.args, hr.shadowed
		FROM hop_rule hr JOIN rule r ON r.id = hr.rule_id
		WHERE hr.hop_id = ? AND r.action_class = 'header' ORDER BY hr.ordinal`, hopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ruleRow
	for rows.Next() {
		var r ruleRow
		if err := rows.Scan(&r.id, &r.directive, &r.args, &r.shadowed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// headerFromArgs pulls the header name and, where the directive states one, the
// value out of a vendor's own syntax.
func headerFromArgs(directive, args string) (name, value string) {
	fields := strings.Fields(args)
	switch strings.ToLower(directive) {
	case "add_header", "more_set_headers", "proxy_set_header":
		if len(fields) > 0 {
			return strings.TrimSuffix(fields[0], ":"), strings.Trim(strings.Join(fields[1:], " "), `"`)
		}
	case "header":
		// Apache: Header always set X-Frame-Options "DENY"
		for i, f := range fields {
			switch strings.ToLower(f) {
			case "set", "append", "add", "always", "onsuccess", "merge", "edit":
				continue
			default:
				return f, strings.Trim(strings.Join(fields[i+1:], " "), `"`)
			}
		}
	case "http-response":
		// HAProxy: http-response set-header X-Foo bar
		for i, f := range fields {
			if strings.HasPrefix(f, "set-header") || strings.HasPrefix(f, "add-header") {
				if i+1 < len(fields) {
					return fields[i+1], strings.Trim(strings.Join(fields[i+2:], " "), `"`)
				}
			}
		}
	}
	return "", ""
}

// ----------------------------------------------------------------- stage three

// correlate is the only path to `verified`. It reads the access log on the target,
// restricted to the paths the Instance's own configuration declares, and looks for
// the correlation token — by token, never by timestamp. Two requests a millisecond
// apart are indistinguishable by time, and a wrong match here is worse than no
// match (probe.md §5).
func (p *Prober) correlate(ctx context.Context, probeID int64, hops []hopRow, res *Result,
	emit func(Step)) Stage {
	st := Stage{Title: "3 · log correlation"}
	if p.Dialer == nil {
		st.Lines = append(st.Lines, "no SSH dialer configured; log correlation skipped")
		return st
	}
	lookback := p.DB.SettingInt(ctx, "probe_log_lookback_seconds")
	verified := 0

	for _, h := range hops {
		if h.External || h.InstanceID == 0 {
			continue
		}
		if gap := p.LogFormatGap(ctx, h.InstanceID, h.Instance, h.Vendor); gap != nil {
			res.Gaps = append(res.Gaps, *gap)
		}
		if len(h.LogPaths) == 0 {
			st.Lines = append(st.Lines,
				h.Instance+": no access log path is derivable from its configuration")
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateMissing,
				Text: h.label() + ": no access log path is derivable from its configuration"})
			p.addEvidence(ctx, probeID, h, 0, "access_log_error",
				"no access log path is derivable from its configuration", nil, "inferred")
			continue
		}
		host, err := p.Dialer.Connect(ctx, h.NodeID)
		if err != nil {
			st.Lines = append(st.Lines, h.Instance+": "+err.Error())
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateMissing,
				Text: h.label() + ": " + err.Error()})
			p.addEvidence(ctx, probeID, h, 0, "access_log_error", err.Error(), nil, "inferred")
			continue
		}
		found, refused := false, false
		var lastPath, lastErr, checkedPath string
		for _, path := range h.LogPaths {
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateChecking,
				Text: "reading " + path + " on " + h.label()})
			out, err := host.Run(ctx, sshx.LogTail(path, logTailBytes, true))
			if err != nil {
				st.Lines = append(st.Lines, h.Instance+" "+path+": "+err.Error())
				emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateMissing,
					Text: h.label() + " " + path + ": " + err.Error()})
				refused, lastPath, lastErr = true, path, err.Error()
				continue
			}
			// A refused read is not an absent token. `tail` is TolerateExit, so a
			// sudoers grant that does not cover the log path comes back as empty
			// stdout, and reporting that as "the token is not there" would blame the
			// fleet for a permission nagipath was never given.
			if out.ExitCode != 0 && strings.TrimSpace(out.Stdout) == "" {
				why := strings.TrimSpace(out.Stderr)
				if why == "" {
					why = fmt.Sprintf("the read exited %d with no output", out.ExitCode)
				}
				st.Lines = append(st.Lines, h.Instance+" "+path+": "+why)
				emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateMissing,
					Text: h.label() + ": " + path + " could not be read — " + why})
				refused, lastPath, lastErr = true, path, why
				continue
			}
			line := findToken(out.Stdout, res.Token)
			if line == "" {
				checkedPath = path
				continue
			}
			p.addEvidence(ctx, probeID, h, 0, "access_log_line", line,
				map[string]any{"log_path": path, "matched_by": "correlation_token",
					"lookback_seconds": lookback}, "verified")
			p.raiseHop(ctx, h.ID, "verified")
			st.Lines = append(st.Lines, h.Instance+": matched in "+path)
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateVerified,
				Text: h.label() + ": the token is in " + path + " — this hop handled the request"})
			verified++
			found = true
			break
		}
		host.Close()
		// Only claim the token is absent when every log was actually read: the reason
		// each read failed has already been stated above. Recorded as evidence, not
		// just a step line, so the reason a hop stayed Inferred survives the page
		// reload the step log does not (probe.md §6).
		if !found && refused {
			p.addEvidence(ctx, probeID, h, 0, "access_log_error", lastErr,
				map[string]any{"log_path": lastPath}, "inferred")
		} else if !found {
			st.Lines = append(st.Lines,
				h.Instance+": the token is not in the last "+bytesWord(logTailBytes)+" of its access log")
			emit(Step{Ordinal: h.Ordinal, Instance: h.Instance, State: StateMissing,
				Text: h.label() + ": the token is not in the last " + bytesWord(logTailBytes) +
					" of its access log"})
			p.addEvidence(ctx, probeID, h, 0, "access_log_absent",
				"the last "+bytesWord(logTailBytes)+" of "+checkedPath+" carried no line with the token",
				map[string]any{"log_path": checkedPath}, "inferred")
		}
	}
	if len(st.Lines) == 0 {
		st.Lines = append(st.Lines, "no internal hop to correlate against")
	}
	if verified > 0 {
		st.Badge = "verified"
	}
	return st
}

// findToken scans for the correlation token. The whole line is kept as raw
// evidence, because a truncated log line cannot be re-read later.
func findToken(stdout, token string) string {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, token) {
			return strings.TrimRight(line, "\r")
		}
	}
	return ""
}

func bytesWord(n int) string { return fmt.Sprintf("%d KiB", n>>10) }

// logFormatGap reports a vendor log format that cannot carry what verification
// needs. nagipath states the directive and stops: it does not edit a managed host,
// and a Verified produced by guessing around a missing field would be a lie.
// LogFormatGap reports whether this instance's log format can identify a request at
// all, with the directive that would fix it. It reads only the current snapshot, so a
// screen can ask it without a Probe having run: the gap is a fact about the
// configuration, and it was already true before anyone sent a request.
func (p *Prober) LogFormatGap(ctx context.Context, instanceID int64, instance, vendor string) *Gap {
	var directives []string
	rows, err := p.DB.R.QueryContext(ctx, `SELECT raw_text FROM rule
		WHERE instance_id = ? AND lower(directive) IN ('log_format','logformat','option')
		  AND snapshot_id = (SELECT id FROM snapshot WHERE instance_id = ? AND is_current = 1)`,
		instanceID, instanceID)
	if err == nil {
		for rows.Next() {
			var raw string
			if rows.Scan(&raw) == nil {
				directives = append(directives, raw)
			}
		}
		rows.Close()
	}
	return logFormatGap(instance, vendor, directives)
}

// logFormatGap is the pure decision behind LogFormatGap, split out so it can be
// tested against directive text directly rather than through a Snapshot fixture.
func logFormatGap(instance, vendor string, directives []string) *Gap {
	all := strings.Join(directives, "\n")
	lower := strings.ToLower(all)
	// Attribution (which site/vhost/backend answered) is necessary but not
	// sufficient: a format can carry $server_name/%v and still have nothing that
	// contains the Probe's own correlation token, in which case a Hop through this
	// Instance can never move past Inferred no matter how many Probes run against
	// it. Both branches below check for the token field as its own condition
	// rather than assuming attribution implies it.
	hasToken := strings.Contains(lower, "x_nagipath_probe") || strings.Contains(lower, "x-nagipath-probe") ||
		strings.Contains(lower, "user_agent") || strings.Contains(lower, "user-agent")

	switch vendor {
	case "nginx":
		missing := []string{}
		if !strings.Contains(all, "$server_name") {
			missing = append(missing, "$server_name")
		}
		if !strings.Contains(all, "$upstream_addr") {
			missing = append(missing, "$upstream_addr")
		}
		if !hasToken {
			missing = append(missing, "$http_x_nagipath_probe (or $http_user_agent)")
		}
		if len(missing) == 0 {
			return nil
		}
		return &Gap{Instance: instance, Vendor: "nginx",
			Note: "Stock `combined` is missing what nagipath needs to verify a Hop here — attribution (server_name/upstream_addr) and/or the Probe's own correlation token. Add this to verify — nagipath will not make the change. Missing: " +
				strings.Join(missing, ", "),
			Directive: `log_format nagipath '$remote_addr - $host [$time_local] "$request" '
                   '$status $body_bytes_sent $server_name $upstream_addr '
                   '"$http_user_agent" $http_x_nagipath_probe';
access_log /var/log/nginx/access.log nagipath;`}
	case "apache":
		missing := []string{}
		if !strings.Contains(all, "%v") {
			missing = append(missing, "%v")
		}
		if !hasToken {
			missing = append(missing, "%{X-Nagipath-Probe}i (or %{User-Agent}i)")
		}
		if len(missing) == 0 {
			return nil
		}
		return &Gap{Instance: instance, Vendor: "apache",
			Note: "The active LogFormat is missing what nagipath needs to verify a Hop here — vhost attribution (%v) and/or the Probe's own correlation token. Add this to verify — nagipath will not make the change. Missing: " +
				strings.Join(missing, ", "),
			Directive: `LogFormat "%h %v %l %u %t \"%r\" %>s %b \"%{User-Agent}i\" %{X-Nagipath-Probe}i" nagipath
CustomLog /var/log/apache2/access.log nagipath`}
	}
	return nil
}

// ------------------------------------------------------------------ persistence

func (p *Prober) insertProbe(ctx context.Context, o Options, method, target, token string) (int64, error) {
	var traceID any
	if o.TraceID > 0 {
		traceID = o.TraceID
	}
	res, err := p.DB.W.ExecContext(ctx, `INSERT INTO probe
		(trace_id, actor_user_id, method, url, correlation_token, origin_host,
		 requested_at, result)
		VALUES (?,?,?,?,?,?,?,'running')`,
		traceID, o.ActorID, method, target, token, o.OriginHost, store.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (p *Prober) finishProbe(ctx context.Context, id int64, res *Result, outcome string) {
	headers := map[string]string{}
	for _, h := range res.Headers {
		headers[h[0]] = h[1]
	}
	hj, _ := json.Marshal(headers)
	rj, _ := json.Marshal(res.Redirects)
	var status any
	if res.Status > 0 {
		status = res.Status
	}
	if _, err := p.DB.W.ExecContext(ctx, `UPDATE probe SET completed_at = ?, status_code = ?,
		redirect_chain = ?, response_headers = ?, duration_ms = ?, result = ?, error = ?
		WHERE id = ?`,
		store.Now(), status, string(rj), string(hj), res.DurationMS, outcome,
		res.Err, id); err != nil {
		p.Log.Warn("probe not recorded", "probe", id, "err", err)
	}
}

func (p *Prober) addEvidence(ctx context.Context, probeID int64, h hopRow, ruleID int64,
	kind, raw string, fields map[string]any, grants string) {
	if fields == nil {
		fields = map[string]any{}
	}
	// What the hop's confidence was before this probe touched it, read at probe start.
	// The before/after table is the point of the screen, and a "before" recomputed when
	// the page is next opened is just the "after" printed twice.
	if _, ok := fields["prior_confidence"]; !ok {
		fields["prior_confidence"] = h.Confidence
	}
	fj, _ := json.Marshal(fields)
	var rule, hop, inst any
	if ruleID > 0 {
		rule = ruleID
	}
	if h.ID > 0 {
		hop = h.ID
	}
	if h.InstanceID > 0 {
		inst = h.InstanceID
	}
	logPath, _ := fields["log_path"].(string)
	if _, err := p.DB.W.ExecContext(ctx, `INSERT INTO probe_evidence
		(probe_id, hop_id, rule_id, instance_id, kind, log_path, raw_evidence,
		 parsed_fields, grants, observed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		probeID, hop, rule, inst, kind, logPath, raw, string(fj), grants,
		store.Now()); err != nil {
		p.Log.Warn("probe evidence not recorded", "probe", probeID, "err", err)
	}
}

// raiseHop only ever raises. A probe that failed to see something does not lower a
// hop nagipath had already verified some other way.
var hopRank = map[string]int{"inferred": 0, "observed_effect": 1, "verified": 2}

func (p *Prober) raiseHop(ctx context.Context, hopID int64, to string) {
	current := p.hopConfidence(ctx, hopID)
	if hopRank[to] <= hopRank[current] {
		return
	}
	p.DB.W.ExecContext(ctx, `UPDATE hop SET confidence = ? WHERE id = ?`, to, hopID)
}

func (p *Prober) hopConfidence(ctx context.Context, hopID int64) string {
	var c string
	p.DB.R.QueryRowContext(ctx, `SELECT confidence FROM hop WHERE id = ?`, hopID).Scan(&c)
	return c
}

var ruleRank = map[string]int{"candidate": 0, "observed_effect": 1, "verified": 2}

func (p *Prober) setHopRuleConfidence(ctx context.Context, hopID, ruleID int64, to string) {
	var c string
	p.DB.R.QueryRowContext(ctx,
		`SELECT confidence FROM hop_rule WHERE hop_id = ? AND rule_id = ?`,
		hopID, ruleID).Scan(&c)
	if ruleRank[to] <= ruleRank[c] {
		return
	}
	p.DB.W.ExecContext(ctx,
		`UPDATE hop_rule SET confidence = ? WHERE hop_id = ? AND rule_id = ?`, to, hopID, ruleID)
}

// refreshTraceConfidence restates the Trace's confidence as the weakest of its
// hops. A trace is only as good as its worst hop, and rounding that up is exactly
// the overclaim the confidence vocabulary exists to prevent.
func (p *Prober) refreshTraceConfidence(ctx context.Context, traceID int64) {
	if traceID == 0 {
		return
	}
	rows, err := p.DB.R.QueryContext(ctx,
		`SELECT confidence FROM hop WHERE trace_id = ? AND is_external = 0`, traceID)
	if err != nil {
		return
	}
	weakest := ""
	for rows.Next() {
		var c string
		if rows.Scan(&c) != nil {
			continue
		}
		if weakest == "" || hopRank[c] < hopRank[weakest] {
			weakest = c
		}
	}
	rows.Close()
	if weakest == "" {
		return
	}
	var partial int
	p.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM trace
		WHERE id = ? AND confidence = 'partial'`, traceID).Scan(&partial)
	if partial > 0 {
		// `partial` is a statement about the snapshots the trace rests on, not about
		// evidence, and a probe cannot make an incomplete snapshot complete.
		return
	}
	p.DB.W.ExecContext(ctx, `UPDATE trace SET confidence = ? WHERE id = ?`, weakest, traceID)
}

func (p *Prober) hops(ctx context.Context, traceID int64) ([]hopRow, error) {
	if traceID == 0 {
		return nil, nil
	}
	rows, err := p.DB.R.QueryContext(ctx, `SELECT h.id, h.ordinal, h.is_external,
		COALESCE(h.instance_id, 0), COALESCE(i.display_name, ''), COALESCE(i.vendor, ''),
		COALESCE(i.node_id, 0), COALESCE(n.display_name, ''),
		COALESCE(i.access_log_paths, '[]'), h.confidence
		FROM hop h LEFT JOIN instance i ON i.id = h.instance_id
		LEFT JOIN node n ON n.id = i.node_id
		WHERE h.trace_id = ? ORDER BY h.ordinal`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []hopRow
	for rows.Next() {
		var h hopRow
		var paths string
		if err := rows.Scan(&h.ID, &h.Ordinal, &h.External, &h.InstanceID, &h.Instance,
			&h.Vendor, &h.NodeID, &h.Node, &paths, &h.Confidence); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(paths), &h.LogPaths)
		out = append(out, h)
	}
	return out, rows.Err()
}

func (p *Prober) version() string {
	if p.Version == "" {
		return "dev"
	}
	return p.Version
}

// ---------------------------------------------------------------------- history

// EvidenceRow is one stored piece of evidence, with the bytes it rests on. The raw
// line is the point: a confidence badge nobody can audit is worthless, so the log
// line that raised the hop is kept verbatim and shown verbatim.
type EvidenceRow struct {
	HopOrdinal int // -1 when the hop row it pointed at is gone
	Instance   string
	Node       string
	Kind       string
	LogPath    string
	Raw        string
	Grants     string
	ObservedAt string
	// Prior is the hop's confidence before this probe ran, empty on rows written before
	// nagipath stored it. ponytail: the screen shows an empty Prior as inferred, which
	// understates what was already known rather than overstating what was proved.
	Prior string
}

// Past is a stored Probe read back out of the database: the same response fields the
// live run reports, plus the evidence rows. The step-by-step progress log is not
// stored — it is one operator watching one request — so what survives is the outcome
// and the bytes behind it, which is what an argument three days later needs.
type Past struct {
	Result
	Outcome  string
	Evidence []EvidenceRow
}

// Record is one past Probe, for the collapsed list under the graph. Every probe ever
// sent at a URL is in it, including the failures: a request that went out and is not
// recorded is the one thing an audit trail cannot tolerate.
type Record struct {
	ID          int64
	Method      string
	URL         string
	Status      sql.NullInt64
	Result      string
	Actor       string
	OriginHost  string
	RequestedAt string
	Evidence    int
	Verified    int
}

// Recent lists probes sent at a URL, newest first.
func Recent(ctx context.Context, db *store.DB, url string, limit int) ([]Record, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT p.id, p.method, p.url, p.status_code,
		p.result, COALESCE(u.username, ''), p.origin_host, p.requested_at,
		(SELECT COUNT(*) FROM probe_evidence e WHERE e.probe_id = p.id),
		(SELECT COUNT(*) FROM probe_evidence e WHERE e.probe_id = p.id AND e.grants = 'verified')
		FROM probe p LEFT JOIN app_user u ON u.id = p.actor_user_id
		WHERE p.url = ? ORDER BY p.requested_at DESC, p.id DESC LIMIT ?`, url, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.Method, &r.URL, &r.Status, &r.Result, &r.Actor,
			&r.OriginHost, &r.RequestedAt, &r.Evidence, &r.Verified); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Verified reports whether this Probe proved that hop handled the request, so a
// graph drawn later still shows the ring. Both the ordinal and the instance must
// match: hop ordinals are only stable while the configuration is, and a green ring
// on the wrong hop is exactly the confident wrong Verified this product refuses.
func (p *Past) Verified(ordinal int, instance string) bool {
	for _, e := range p.Evidence {
		if e.Grants == "verified" && e.HopOrdinal == ordinal && e.Instance == instance {
			return true
		}
	}
	return false
}

// Raised reports whether this Probe moved that hop off inferred at all — a response
// header it declares is enough, an access-log line is better. Matched by ordinal and
// instance both, for the same reason Verified is.
func (p *Past) Raised(ordinal int, instance string) bool {
	for _, e := range p.Evidence {
		if (e.Grants == "verified" || e.Grants == "observed_effect") &&
			e.HopOrdinal == ordinal && e.Instance == instance {
			return true
		}
	}
	return false
}

// Last returns the most recent Probe sent at a URL, or nil when there is none.
func Last(ctx context.Context, db *store.DB, url string) (*Past, error) {
	if url == "" {
		return nil, nil
	}
	var p Past
	var hj, rj string
	err := db.R.QueryRowContext(ctx, `SELECT p.id, p.method, p.url,
		COALESCE(p.status_code, 0), p.correlation_token, p.origin_host, p.requested_at,
		COALESCE(p.duration_ms, 0), p.result, p.error, p.redirect_chain, p.response_headers,
		COALESCE(u.username, '')
		FROM probe p LEFT JOIN app_user u ON u.id = p.actor_user_id
		WHERE p.url = ? ORDER BY p.requested_at DESC, p.id DESC LIMIT 1`, url).
		Scan(&p.ProbeID, &p.Method, &p.URL, &p.Status, &p.Token, &p.OriginHost,
			&p.RequestedAt, &p.DurationMS, &p.Outcome, &p.Err, &rj, &hj, &p.ActorLabel)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if p.Status > 0 {
		p.StatusText = strconv.Itoa(p.Status) + " " + http.StatusText(p.Status)
	}
	json.Unmarshal([]byte(rj), &p.Redirects)
	// Stored as an object, so the order the server sent them in is already lost.
	// Sorted by name, which is at least the same order every time it is read.
	var hm map[string]string
	json.Unmarshal([]byte(hj), &hm)
	names := make([]string, 0, len(hm))
	for k := range hm {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		p.Headers = append(p.Headers, [2]string{k, hm[k]})
	}

	rows, err := db.R.QueryContext(ctx, `SELECT COALESCE(h.ordinal, -1),
		COALESCE(i.display_name, ''), COALESCE(n.display_name, ''), e.kind, e.log_path,
		e.raw_evidence, e.grants, e.observed_at,
		COALESCE(json_extract(e.parsed_fields, '$.prior_confidence'), '')
		FROM probe_evidence e
		LEFT JOIN hop h ON h.id = e.hop_id
		LEFT JOIN instance i ON i.id = e.instance_id
		LEFT JOIN node n ON n.id = i.node_id
		WHERE e.probe_id = ? ORDER BY COALESCE(h.ordinal, 1000), e.id`, p.ProbeID)
	if err != nil {
		return &p, err
	}
	defer rows.Close()
	for rows.Next() {
		var e EvidenceRow
		if err := rows.Scan(&e.HopOrdinal, &e.Instance, &e.Node, &e.Kind, &e.LogPath,
			&e.Raw, &e.Grants, &e.ObservedAt, &e.Prior); err != nil {
			return &p, err
		}
		p.Evidence = append(p.Evidence, e)
	}
	return &p, rows.Err()
}
