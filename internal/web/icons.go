package web

import "html/template"

// The sidebar icons. Inline SVG rather than an icon font or a sprite file: it is
// one HTTP request fewer, it inherits currentColor so the active item needs no
// second rule, and it does not trip the script-src CSP the way an <object> or a
// JS icon library would.
//
// The wireframe drew these as empty 13px boxes — a placeholder for "an icon goes
// here", which is what shipped when the sidebar was ported literally.
//
// ponytail: hand-written paths on a 16-unit grid, stroked, no fills. A real icon
// set is a dependency and a build step for ten glyphs; add one when the product
// needs a hundred.
var navIcons = map[string]string{
	// four panes — the dashboard is a grid of them
	"dashboard": `<path d="M2 2h5v5H2zM9 2h5v5H9zM2 9h5v5H2zM9 9h5v5H9z"/>`,
	// a request curving through the fleet, arrowhead at the far end
	"trace": `<path d="M2 13c5 0 3-10 10-10"/><path d="M9.5 1.2 12.5 3l-3 1.8"/>`,
	// a funnel: rule lookup narrows every rule in the fleet down to the ones that fire
	"rules": `<path d="M2 3h12L9.5 8.5V14l-3-2V8.5z"/>`,
	// a magnifier over raw config text
	"search": `<circle cx="7" cy="7" r="4.6"/><path d="M10.4 10.4 14 14"/>`,
	// a globe: sites are hostnames
	"sites": `<circle cx="8" cy="8" r="6"/><path d="M2 8h12"/><path d="M8 2c3.2 3.4 3.2 8.6 0 12"/><path d="M8 2c-3.2 3.4-3.2 8.6 0 12"/>`,
	// two stacked units with a status light each
	"nodes": `<path d="M2.5 2.5h11v4h-11zM2.5 9.5h11v4h-11z"/><path d="M4.6 4.5h.01M4.6 11.5h.01"/>`,
	// layers: a cluster is one configuration on several nodes
	"clusters": `<path d="M8 1.8l6 2.9-6 2.9-6-2.9z"/><path d="M2 8.2l6 2.9 6-2.9"/><path d="M2 11.4l6 2.9 6-2.9"/>`,
	// two arrows out of step — one node no longer matches its baseline
	"drift": `<path d="M5 3.2v9M2.6 5.6 5 3.2l2.4 2.4"/><path d="M11 12.8v-9M13.4 10.4 11 12.8l-2.4-2.4"/>`,
	// a shield with a check: a certificate is a claim someone vouched for
	"certificates": `<path d="M8 14s5-2.2 5-6.2V3.9L8 2 3 3.9v3.9C3 11.8 8 14 8 14z"/><path d="M5.9 7.7 7.5 9.3 10.4 6.4"/>`,
	// sliders: settings are the handful of values an operator owns
	"settings": `<path d="M2 5h3M9.5 5H14M2 11h6.5M13 11h1"/><circle cx="7.2" cy="5" r="1.8"/><circle cx="10.8" cy="11" r="1.8"/>`,
}

// icon renders one sidebar glyph. An unknown name renders nothing rather than a
// box: a placeholder that looks like a control is worse than a label on its own.
func icon(name string) template.HTML {
	path, ok := navIcons[name]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="ic" viewBox="0 0 16 16" aria-hidden="true" ` +
		`fill="none" stroke="currentColor" stroke-width="1.4" ` +
		`stroke-linecap="round" stroke-linejoin="round">` + path + `</svg>`)
}
