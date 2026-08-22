package web

import "strings"

// The shapes behind the shared partials in templates/parts. A partial takes one
// argument, so anything with more than one field gets a constructor here and is
// built in the template: {{template "cards" cards (card .Total "nodes" "/nodes")}}.
// Keeping the types tiny is deliberate — a component that needs a struct with ten
// fields is a page, not a component.

// Card is one figure in the row a page leads with: a count, what it counts, and
// where to go to read it. Tone is "" , "warn" or "dashed".
type Card struct {
	N     int
	Label string
	Href  string
	Tone  string
	skip  bool
}

func card(n int, label, href, tone string) Card {
	return Card{N: n, Label: label, Href: href, Tone: tone}
}

// cardif is a card with nothing to report when its condition is false — a screen
// that leads with "0 host keys to approve" is reporting good news nobody asked
// for. cards drops it.
func cardif(show bool, n int, label, href, tone string) Card {
	c := card(n, label, href, tone)
	c.skip = !show
	return c
}

func cards(list ...Card) []Card {
	out := make([]Card, 0, len(list))
	for _, c := range list {
		if !c.skip {
			out = append(out, c)
		}
	}
	return out
}

// warnif is the tone a count deserves: nothing to say when it is zero. It exists
// so the decision is in the template beside the number rather than in a class
// expression repeated on every card.
func warnif(n int) string {
	if n > 0 {
		return "warn"
	}
	return ""
}

// column is one table heading. The width is on the heading because a table whose
// columns resize as data changes is unreadable across page loads.
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

// toolbarData is the title bar every screen opens with. Pages whose toolbar
// carries breadcrumb links or badges beside the title build their own; this
// covers the plain title-and-caption case, which is most of them.
type toolbarData struct {
	Title string
	Crumb string
}

func toolbar(title, crumb string) toolbarData {
	return toolbarData{Title: title, Crumb: crumb}
}
