package web

import (
	"html/template"

	"github.com/nagiflow/nagipath/internal/store"
)

// funcs is the template FuncMap for the handful of screens internal/web still
// renders server-side (setup, login, password, 404, and Import inventory —
// see inventory_import.go). Everything else moved to the React SPA
// (docs/adr/0017); the business-logic helpers that used to live in this file
// (rule/trace/probe formatting, badge tone mapping, certificate SAN coverage,
// and so on) moved with it into internal/api, computed once there rather than
// recomputed in TypeScript (docs/adr/0018).
var funcs = template.FuncMap{
	"toolbar": toolbar,
	"cols":    cols,
	"ic":      icon,
	"num":     num,
}

// procName is the process label shown next to a node's hostname and in the
// process picker. Vendor alone is what an operator thinks of ("this box runs
// nginx"); the config file name only earns a place when the node runs more
// than one instance of the same vendor and the file is what tells them apart.
func procName(instances []store.Instance, vendor, displayName string) string {
	n := 0
	for _, in := range instances {
		if in.Vendor == vendor {
			n++
		}
	}
	if n > 1 {
		return displayName
	}
	return vendor
}
