package web

import (
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// The left navigation, as data. It is a table rather than markup in layout.html
// because three things read it: the sidebar, the active-item highlight, and the
// breadcrumb in the header. Deriving all three from one list is what keeps them
// from disagreeing after a route is renamed.

type navItem struct {
	Label string
	Href  string
	Count int
	// Warn tints the count amber: a number an operator is meant to act on.
	Warn bool
	// Admin hides the item from viewers. Viewers read everything the fleet
	// contains; Settings is where the writes are.
	Admin bool
	// Under are extra path prefixes that light this item up — /nodes/42 and
	// /nodes/42/routes both belong to Nodes.
	Under []string
	// Icon names the glyph in navIcons. Empty renders no glyph, which is what the
	// second-level Settings list wants.
	Icon string
	On    bool
}

type navGroup struct {
	Label string
	Items []navItem
	// Foot pins the group to the bottom of the sidebar, past the flex spacer.
	Foot bool
}

// nav builds the sidebar for one request. Counts come from a single query set
// (store.NavCounts); a zero count renders as nothing rather than as "0", since
// "Clusters 0" is a fact nobody needs on every screen.
func nav(path string, u store.User, c store.NavCount) []navGroup {
	groups := []navGroup{
		{Label: "Explore", Items: []navItem{
			{Label: "Dashboard", Href: "/", Icon: "dashboard"},
			{Label: "Trace", Href: "/trace", Under: []string{"/probe"}, Icon: "trace"},
			{Label: "Rule lookup", Href: "/rules", Icon: "rules"},
			{Label: "Config search", Href: "/search", Icon: "search"},
		}},
		{Label: "Inventory", Items: []navItem{
			{Label: "Sites", Href: "/sites", Count: c.Sites, Icon: "sites"},
			{Label: "Nodes", Href: "/nodes", Count: c.Nodes, Icon: "nodes"},
			{Label: "Clusters", Href: "/clusters", Count: c.Clusters, Icon: "clusters"},
		}},
		{Label: "Analysis", Items: []navItem{
			{Label: "Drift", Href: "/drift", Count: c.Drift, Warn: c.Drift > 0, Icon: "drift"},
			{Label: "Certificates", Href: "/certificates", Count: c.Certificates, Warn: c.Certificates > 0, Icon: "certificates"},
		}},
		{Foot: true, Items: []navItem{
			{Label: "Settings", Href: "/settings", Icon: "settings", Under: []string{"/settings/", "/collections", "/password"}},
		}},
	}
	out := make([]navGroup, 0, len(groups))
	for _, g := range groups {
		items := make([]navItem, 0, len(g.Items))
		for _, it := range g.Items {
			if it.Admin && !u.IsAdmin() {
				continue
			}
			it.On = navMatch(path, it)
			items = append(items, it)
		}
		g.Items = items
		out = append(out, g)
	}
	return out
}

// navMatch decides the highlight. "/" only ever matches itself; every other item
// owns its subtree, so /instances/42/routes highlights Instances.
func navMatch(path string, it navItem) bool {
	if it.Href == "/" {
		return path == "/"
	}
	if path == it.Href || strings.HasPrefix(path, it.Href+"/") {
		return true
	}
	for _, p := range it.Under {
		if path == p || strings.HasPrefix(path, strings.TrimSuffix(p, "/")+"/") {
			return true
		}
	}
	return false
}

// crumb is the header's "Explore / Trace". The group and the item come from the
// same table the sidebar does; a page that is deeper than an item — an instance,
// a snapshot file — appends its own tail by setting page.Crumb.
func crumb(groups []navGroup) (string, string) {
	for _, g := range groups {
		for _, it := range g.Items {
			if it.On {
				return g.Label, it.Label
			}
		}
	}
	return "", ""
}

// settingsNav is the second-level list on Settings. Its sections are separate
// pages rather than tabs on one, because Users and Audit are two very different
// queries and one screen holding both would load both.
//
// Sections a viewer may not open are dropped, not disabled: an item that
// refuses every click is worse than an item that is not there.
func settingsNav(path string, u store.User) []navItem {
	items := []navItem{
		{Label: "Credentials", Href: "/settings/credentials", Admin: true},
		{Label: "Host keys", Href: "/settings/hostkeys", Admin: true},
		{Label: "Master key", Href: "/settings/masterkey", Admin: true},
		{Label: "Collection defaults", Href: "/settings/collection-defaults", Admin: true},
		{Label: "Collection jobs", Href: "/collections", Admin: true},
		{Label: "Retention", Href: "/settings/retention", Admin: true},
		{Label: "Users & roles", Href: "/settings/users", Admin: true},
		{Label: "API keys", Href: "/settings/api-keys", Admin: true},
		{Label: "Audit log", Href: "/settings/audit"},
		{Label: "License", Href: "/settings/license"},
		{Label: "System", Href: "/settings/system", Admin: true},
		{Label: "Your account", Href: "/password"},
	}
	out := make([]navItem, 0, len(items))
	for _, it := range items {
		if it.Admin && !u.IsAdmin() {
			continue
		}
		it.On = path == it.Href
		out = append(out, it)
	}
	return out
}

// initials fills the header avatar. Two letters from a username, because the
// product has no display-name field and inventing one for a monogram would be
// a schema change in service of decoration.
func initials(username string) string {
	parts := strings.FieldsFunc(username, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == '@' || r == ' '
	})
	switch {
	case len(parts) == 0:
		return "?"
	case len(parts) == 1:
		if len(parts[0]) == 1 {
			return strings.ToUpper(parts[0])
		}
		return strings.ToUpper(parts[0][:2])
	default:
		return strings.ToUpper(parts[0][:1] + parts[1][:1])
	}
}
