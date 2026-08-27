package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

func (s *Server) apiAuth(next func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(raw) == "" {
			apiError(w, http.StatusUnauthorized, "unauthenticated", "A Bearer API key is required.")
			return
		}
		u, err := s.DB.AuthenticateAPIToken(r.Context(), strings.TrimSpace(raw))
		if err != nil {
			apiError(w, http.StatusUnauthorized, "invalid_token", "The API key is invalid, expired or revoked.")
			return
		}
		next(w, r, u)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) apiNodes(w http.ResponseWriter, r *http.Request, _ store.User) {
	list, err := s.DB.Nodes(r.Context())
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, n := range list {
		items = append(items, map[string]any{
			"id": n.ID, "name": n.DisplayName, "address": n.Address,
			"ssh_port": n.SSHPort, "os_family": n.OSFamily, "enabled": n.Enabled,
			"instances": n.InstanceCount, "pending_host_keys": n.PendingHostKeys,
			"consecutive_failures": n.ConsecutiveFailures,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total_estimate": len(items)})
}

func (s *Server) apiClusters(w http.ResponseWriter, r *http.Request, _ store.User) {
	list, err := s.DB.Clusters(r.Context())
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, c := range list {
		items = append(items, map[string]any{
			"id": c.ID, "name": c.Name, "description": c.Description,
			"members": c.Members, "baseline": c.GoldenPeerName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total_estimate": len(items)})
}

func (s *Server) apiDrift(w http.ResponseWriter, r *http.Request, _ store.User) {
	list, err := s.DB.DriftRuns(r.Context(), 0, "")
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, run := range list {
		items = append(items, map[string]any{
			"id": run.ID, "instance_id": run.InstanceID, "instance": run.InstanceName,
			"node": run.NodeName, "vendor": run.Vendor, "baseline": run.BaselineKind,
			"computed_at": run.ComputedAt, "findings": run.FindingCount,
			"ignored": run.IgnoredCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total_estimate": len(items)})
}
