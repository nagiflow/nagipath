package web

import "net/http"

// Collections and Settings.
//
// Settings is a page per section rather than tabs on one, because Users and the
// Audit log are two very different queries and a single screen holding both
// would run both on every visit. The sub-navigation is settingsNav in nav.go.
func (s *Server) routesOps(m *http.ServeMux) {
	m.HandleFunc("GET /collections", s.auth(s.collections))

	// Host key approval is the gate on running any command on a Node, so it is
	// its own action with its own audit entry, reachable from Settings.
	m.HandleFunc("POST /hostkeys/{id}/decide", s.admin(s.decideHostKey))
	m.HandleFunc("POST /hostkeys/approve", s.admin(s.approveHostKeys))

	// Settings index redirects to the first section.
	// Only a redirect, so it is auth not admin: a viewer reaching /settings must
	// land on the one section they can read, not a 403 from a page that would have
	// sent them there anyway.
	m.HandleFunc("GET /settings", s.auth(s.settingsIndex))

	m.HandleFunc("GET /settings/credentials", s.admin(s.credentials))
	m.HandleFunc("POST /settings/credentials", s.admin(s.addCredential))

	m.HandleFunc("GET /settings/hostkeys", s.admin(s.hostKeys))

	m.HandleFunc("GET /settings/masterkey", s.admin(s.masterKey))

	m.HandleFunc("GET /settings/collection-defaults", s.admin(s.collectionDefaults))
	m.HandleFunc("POST /settings/collection-defaults", s.admin(s.setCollectionDefaults))

	m.HandleFunc("GET /settings/retention", s.admin(s.retention))
	m.HandleFunc("POST /settings/retention", s.admin(s.setRetention))
	m.HandleFunc("POST /settings/retention/prune", s.admin(s.runRetention))

	m.HandleFunc("GET /settings/users", s.admin(s.users))
	m.HandleFunc("POST /settings/users", s.admin(s.addUser))
	m.HandleFunc("POST /settings/users/{id}/disable", s.admin(s.disableUser))
	m.HandleFunc("POST /settings/users/{id}/enable", s.admin(s.enableUser))

	m.HandleFunc("GET /settings/audit", s.auth(s.audit))

	m.HandleFunc("GET /settings/api-keys", s.admin(s.apiKeys))
	m.HandleFunc("POST /settings/api-keys", s.admin(s.createAPIKey))
	m.HandleFunc("POST /settings/api-keys/{id}/revoke", s.admin(s.revokeAPIKey))

	// A viewer can read license status; only an admin can install one.
	m.HandleFunc("GET /settings/license", s.auth(s.license))
	m.HandleFunc("POST /settings/license", s.admin(s.installLicense))

	// Admin-only, unlike the license page: a diagnostics bundle is exactly the
	// kind of thing a viewer should not be handed a download link for.
	m.HandleFunc("GET /settings/system", s.admin(s.diagnostics))
	m.HandleFunc("GET /settings/system/bundle", s.admin(s.diagnosticsBundle))

	// Where these lived before Settings gathered them up.
	m.HandleFunc("GET /credentials", moved("/settings/credentials"))
	m.HandleFunc("GET /users", moved("/settings/users"))
	m.HandleFunc("GET /audit", moved("/settings/audit"))
	m.HandleFunc("GET /license", moved("/settings/license"))
	m.HandleFunc("GET /diagnostics", moved("/settings/system"))
	m.HandleFunc("GET /diagnostics/bundle", moved("/settings/system/bundle"))
}
