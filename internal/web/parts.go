package web

import (
	"strconv"
	"strings"
)

// The shapes behind the shared partials in templates/parts. A partial takes one
// argument, so anything with more than one field gets a constructor here and is
// built in the template: {{template "stats" stats (stat .Total "nodes" "/nodes" "")}}.
// Keeping the types tiny is deliberate — a component that needs a struct with
// ten fields is a page, not a component.

// Stat is one figure in the row a screen leads with: a count, what it counts,
// and where to go to read it. Tone is "", "warn" or "err".
type Stat struct {
	N     int
	Label string
	Href  string
	Tone  string
	// Note is the sub-line under the number: what the count is of, when the
	// number alone is ambiguous. "9" over "≤ 30 days" does not say whether that
	// is nine certificates or nine bindings.
	Note string
	skip bool
}

func stat(n int, label, href, tone string) Stat {
	return Stat{N: n, Label: label, Href: href, Tone: tone}
}

// note fills the sub-line. It takes the Stat last so it reads as a pipeline —
// `stat 9 "certs ≤ 30d" "/certificates" "warn" | note "88 bindings"` — rather
// than as a five-argument constructor nobody can read positionally.
func note(text string, s Stat) Stat {
	s.Note = text
	return s
}

// statif is a stat with nothing to report when its condition is false — a screen
// that leads with "0 host keys to approve" is reporting good news nobody asked
// for, in the slot the real problem should occupy. stats drops it.
func statif(show bool, n int, label, href, tone string) Stat {
	s := stat(n, label, href, tone)
	s.skip = !show
	return s
}

func stats(list ...Stat) []Stat {
	out := make([]Stat, 0, len(list))
	for _, s := range list {
		if !s.skip {
			out = append(out, s)
		}
	}
	return out
}

// warnif is the tone a count deserves: nothing to say when it is zero. It exists
// so the decision sits in the template beside the number rather than in a class
// expression repeated on every stat.
func warnif(n int) string {
	if n > 0 {
		return "warn"
	}
	return ""
}

// column is one table heading. The width is on the heading because a table whose
// columns resize as its data changes is unreadable across page loads.
type column struct {
	Label string
	Width string
}

// cols reads "label" or "label:width" specs, so a heading row is one line:
// {{template "cols" cols "node" "vendor:6rem" ":1.4rem"}}
func cols(specs ...string) []column {
	out := make([]column, 0, len(specs))
	for _, spec := range specs {
		c := column{Label: spec}
		if i := strings.LastIndex(spec, ":"); i >= 0 {
			c.Label, c.Width = spec[:i], spec[i+1:]
		}
		out = append(out, c)
	}
	return out
}

// toolbarData is the title bar every screen opens with. Pages whose title bar
// carries breadcrumb links or badges beside the title build their own; this
// covers the plain title-and-caption case, which is most of them.
type toolbarData struct {
	Title string
	Crumb string
}

func toolbar(title, crumb string) toolbarData {
	return toolbarData{Title: title, Crumb: crumb}
}

// ----------------------------------------------------------------- badges

// badgeClass maps a confidence — or any state word a screen carries in a badge —
// onto the one of six tones app.css defines. Unknown values get the hollow
// neutral rather than a guess, so a new confidence value added to the walker
// shows up as plainly unstyled instead of quietly reading as "fine".
func badgeClass(state string) string {
	switch state {
	// "ok" and "parsed" are the resting states of a snapshot and an instance, and
	// they were falling through to the hollow neutral — the state most rows carry
	// read as "nagipath has no opinion".
	case "verified", "conforming", "succeeded", "approved", "valid", "ok", "parsed":
		return "ok"
	case "observed_effect", "partial", "degraded", "expiring", "stale", "pending", "drift":
		return "deg"
	case "disproved", "failed", "expired", "quarantined", "denied", "rejected":
		return "err"
	case "inferred", "candidate", "missing":
		return "inf"
	// A fleet past its node ceiling is a commercial fact, not a broken one: the
	// product keeps working (ADR-0014), so it warns rather than reads as failed.
	case "over_ceiling":
		return "deg"
	case "external", "external_hop":
		return "ext"
	case "pool", "dns", "vip", "note", "running":
		return "note"
	}
	return "none"
}

// badgeLabel is the word. Uppercase because the badge is a stamp on a dense row,
// not prose; underscores become spaces so stored codes need no display column.
func badgeLabel(state string) string {
	return strings.ToUpper(strings.ReplaceAll(state, "_", " "))
}

// badgeWhy is the hover text. A badge that does not explain itself trains people
// to ignore it, so every state the walker can produce has a sentence here.
func badgeWhy(state string) string {
	switch state {
	case "verified":
		return "confirmed by evidence from the vendor's own access log, correlated to a probe"
	case "observed_effect":
		return "the result is visible in a probe's response, but the access log did not confirm the hop"
	case "inferred":
		return "derived from configuration alone — the default state of every hop"
	case "candidate":
		return "configuration says this could apply to the path; nothing confirms it did"
	case "partial":
		return "mixed across children; the weakest of them is shown"
	case "disproved":
		return "configured, but a probe proved it is not in effect"
	case "external", "external_hop":
		return "the destination is not a managed instance — the trace leaves the fleet here"
	case "degraded":
		return "collected, but the vendor's own tooling was unavailable, so this came from parsed files"
	case "ok", "parsed":
		return "collected with the vendor's own tooling and parsed without error"
	case "succeeded":
		return "the run reached the host, captured its configuration and stored a snapshot"
	case "failed":
		return "the run did not produce a snapshot — the outcome column says why"
	case "pending":
		return "discovered, but nothing has been collected from it yet"
	case "valid":
		return "signed by the key built into this binary, inside its expiry, within its node ceiling"
	case "missing":
		return "no license file was found — nagipath keeps working, and says so here"
	case "expired":
		return "past its expiry date; enforcement is a notice, never a refusal to serve"
	case "over_ceiling":
		return "more nodes are managed than this license covers"
	}
	return reasonText(state)
}

// num formats a count for the nav and for a stat: thousands separated, because
// 1164 and 11640 are the same shape at a glance and 1,164 and 11,640 are not.
func num(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + num(-n)
	}
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
