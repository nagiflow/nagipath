package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	pb "github.com/nagiflow/nagipath/internal/api/pb/nagipath/api/v1"
	"github.com/nagiflow/nagipath/internal/store"
)

// Version is stamped by internal/web.Version — set once in api.go's New so
// the diagnostics page (this file's getDiagnostics) can report the same
// build identifier the Probe User-Agent uses, without this package importing
// internal/web (which already imports this one).
var Version = "dev"

// ---------------------------------------------------------------- credentials

func (s *Server) getCredentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	typeFilter := r.URL.Query().Get("type")

	kinds, err := s.DB.CredentialAuthKinds(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	list, err := s.DB.CredentialsWithUsage(ctx, typeFilter)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	resp := &pb.CredentialsResponse{AuthKinds: kinds}
	for _, c := range list {
		resp.Credentials = append(resp.Credentials, &pb.CredentialItem{
			Id: c.ID, Name: c.Name, Username: c.Username, AuthKind: c.AuthKind,
			PublicKey: c.PublicKey, Fingerprint: c.Fingerprint, ExternalRef: c.ExternalRef,
			CreatedAt: c.CreatedAt, NodeCount: int32(c.NodeCount), LastUsed: c.LastUsed,
		})
	}
	writeProto(w, http.StatusOK, resp)
}

func (s *Server) postAddCredential(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		AuthKind    string `json:"authKind"`
		Username    string `json:"username"`
		PrivateKey  string `json:"privateKey"`
		Passphrase  string `json:"passphrase"`
		Certificate string `json:"certificate"`
		Password    string `json:"password"`
		ExternalRef string `json:"externalRef"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	if body.Name == "" {
		apiError(w, http.StatusUnprocessableEntity, "invalid_request", "A credential name is required.")
		return
	}
	kind := body.AuthKind
	if kind == "" {
		kind = "private_key"
	}
	ctx, u := r.Context(), userOf(r)
	var (
		id  int64
		err error
	)
	switch kind {
	case "private_key", "ssh_certificate":
		if body.PrivateKey == "" {
			apiError(w, http.StatusUnprocessableEntity, "invalid_request", "A private key is required.")
			return
		}
		if kind == "ssh_certificate" && body.Certificate == "" {
			apiError(w, http.StatusUnprocessableEntity, "invalid_request", "An SSH certificate is required.")
			return
		}
		id, err = s.DB.CreateCredential(ctx, s.Master, body.Name, body.Username, kind, body.PrivateKey,
			body.Passphrase, body.Certificate, &u.ID)
	case "username_password", "ldap", "kerberos":
		id, err = s.DB.CreatePasswordCredential(ctx, s.Master, body.Name, body.Username, kind,
			body.Password, body.ExternalRef, &u.ID)
	case "cyberark":
		id, err = s.DB.CreateCyberArkCredential(ctx, body.Name, body.Username, body.ExternalRef, &u.ID)
	default:
		apiError(w, http.StatusUnprocessableEntity, "invalid_request", "Choose a supported credential type.")
		return
	}
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "create_failed", err.Error())
		return
	}
	_ = s.DB.Audit(ctx, &u.ID, "credential.create", "credential", &id, body.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// ---------------------------------------------------------------- host keys

func (s *Server) getHostKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	stats, err := s.DB.HostKeyStats(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	keys, err := s.DB.AllHostKeys(ctx, q.Get("state"), q.Get("cluster"))
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.HostKeysResponse{
		Stats: &pb.HostKeyStatsPB{Pending: int32(stats.Pending), Changed: int32(stats.Changed),
			Approved: int32(stats.Approved), Algorithms: map[string]int32{}},
	}
	for algo, n := range stats.Algorithms {
		resp.Stats.Algorithms[algo] = int32(n)
	}
	for _, k := range keys {
		resp.Keys = append(resp.Keys, &pb.PendingHostKey{
			Key: &pb.HostKey{Id: k.ID, Algorithm: k.Algorithm,
				Fingerprint: k.Fingerprint, State: k.State, FirstSeenAt: k.FirstSeenAt},
			NodeName: k.NodeName, NodeAddress: k.NodeAddress, Previous: k.Previous,
		})
	}
	writeProto(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------- master key

func (s *Server) getMasterKey(w http.ResponseWriter, r *http.Request) {
	counts, err := s.DB.MasterKeyEncryptedCounts(r.Context())
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.MasterKeyResponse{
		Path: s.Master.Path, CredentialsEncrypted: int32(counts.Credentials),
		ApiKeysEncrypted: int32(counts.APIKeys), JobLogsEncrypted: int32(counts.JobLogs),
	}
	if filepath.IsAbs(s.Master.Path) {
		resp.DataDir = filepath.Dir(s.Master.Path)
	}
	writeProto(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------- retention

func (s *Server) getRetention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := s.DB.RetentionStats(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.RetentionResponse{
		SnapshotDays:   int32(s.DB.SettingInt(ctx, "snapshot_retention_days")),
		MinPerInstance: int32(s.DB.SettingInt(ctx, "snapshot_retention_min_per_instance")),
		JobLogDays:     int32(s.DB.SettingInt(ctx, "job_log_retention_days")),
		ProbeDays:      int32(s.DB.SettingInt(ctx, "probe_retention_days")),
		AuditDays:      int32(s.DB.SettingInt(ctx, "audit_retention_days")),
		Stats: &pb.RetentionStatsPB{
			IndexSizeBytes: stats.IndexSizeBytes, Snapshots: int32(stats.Snapshots),
			JobLogs: int32(stats.JobLogs), Traces: int32(stats.Traces),
		},
	}
	if p := stats.LastPrune; p != nil {
		resp.Stats.LastPrune = &pb.PruneHistoryPB{
			RanAt: p.RanAt, DurationSeconds: p.Duration, FreedBytes: p.Freed,
			Examined: &pb.PruneCountsPB{Snapshots: int32(p.Examined.Snapshots), JobLogs: int32(p.Examined.JobLogs),
				Traces: int32(p.Examined.Traces), AuditEvents: int32(p.Examined.AuditEvents)},
			Deleted: &pb.PrunedCountsPB{Snapshots: p.Deleted.Snapshots, JobLogs: p.Deleted.JobLogs,
				Probes: p.Deleted.Probes, AuditEvents: p.Deleted.AuditEvents, Blobs: p.Deleted.Blobs},
			Kept: &pb.PruneCountsPB{Snapshots: int32(p.Kept.Snapshots), JobLogs: int32(p.Kept.JobLogs),
				Traces: int32(p.Kept.Traces), AuditEvents: int32(p.Kept.AuditEvents)},
		}
	}
	writeProto(w, http.StatusOK, resp)
}

func (s *Server) postRetention(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SnapshotDays   *int `json:"snapshotDays"`
		MinPerInstance *int `json:"minPerInstance"`
		JobLogDays     *int `json:"jobLogDays"`
		ProbeDays      *int `json:"probeDays"`
		AuditDays      *int `json:"auditDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	ctx, u := r.Context(), userOf(r)
	for _, f := range []struct {
		val *int
		key string
		max int
	}{
		{body.SnapshotDays, "snapshot_retention_days", 3650},
		{body.MinPerInstance, "snapshot_retention_min_per_instance", 1000},
		{body.JobLogDays, "job_log_retention_days", 3650},
		{body.ProbeDays, "probe_retention_days", 3650},
		{body.AuditDays, "audit_retention_days", 3650},
	} {
		if f.val == nil {
			continue
		}
		if *f.val < 0 || *f.val > f.max {
			apiError(w, http.StatusUnprocessableEntity, "invalid_request",
				"Each window must be a whole number of days between 0 and "+strconv.Itoa(f.max)+".")
			return
		}
		if err := s.DB.SetSetting(ctx, f.key, strconv.Itoa(*f.val), &u.ID); err != nil {
			apiError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
	}
	_ = s.DB.Audit(ctx, &u.ID, "retention.update", "setting", nil, "retention windows")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) postRunRetention(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	start := time.Now()
	result, err := s.DB.Prune(ctx)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "prune_failed", err.Error())
		return
	}
	dur := time.Since(start).Seconds()
	history := store.PruneHistory{RanAt: time.Now().UTC().Format(time.RFC3339), Duration: dur, Deleted: result}
	_ = s.DB.SavePruneHistory(ctx, history, &u.ID)
	_ = s.DB.Audit(ctx, &u.ID, "retention.run", "setting", nil, "manual prune")
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "snapshots": result.Snapshots, "jobLogs": result.JobLogs,
		"probes": result.Probes, "auditEvents": result.AuditEvents, "blobs": result.Blobs,
	})
}

// ---------------------------------------------------------------- collection defaults

func (s *Server) getCollectionDefaults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := s.DB.Credentials(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	defaultCred, _ := strconv.ParseInt(s.DB.Setting(ctx, "default_credential_id"), 10, 64)
	resp := &pb.CollectionDefaultsResponse{
		DefaultCredential: defaultCred,
		IntervalMinutes:   int32(s.DB.SettingInt(ctx, "collection_interval_seconds") / 60),
		JitterSeconds:     int32(s.DB.SettingInt(ctx, "collection_jitter_seconds")),
		SshWorkers:        int32(s.DB.SettingInt(ctx, "ssh_workers")),
		CommandTimeout:    int32(s.DB.SettingInt(ctx, "ssh_command_timeout_seconds")),
		MaxFiles:          int32(s.DB.SettingInt(ctx, "snapshot_max_files")),
		ProbeRedirects:    int32(s.DB.SettingInt(ctx, "probe_max_redirects")),
		ProbeLookback:     int32(s.DB.SettingInt(ctx, "probe_log_lookback_seconds")),
	}
	for _, c := range creds {
		resp.Credentials = append(resp.Credentials, &pb.CredentialItem{
			Id: c.ID, Name: c.Name, Username: c.Username, AuthKind: c.AuthKind, CreatedAt: c.CreatedAt,
		})
	}
	writeProto(w, http.StatusOK, resp)
}

func (s *Server) postCollectionDefaults(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IntervalMinutes   *int   `json:"intervalMinutes"`
		JitterSeconds     *int   `json:"jitterSeconds"`
		SSHWorkers        *int   `json:"sshWorkers"`
		CommandTimeout    *int   `json:"commandTimeout"`
		MaxFiles          *int   `json:"maxFiles"`
		ProbeRedirects    *int   `json:"probeRedirects"`
		ProbeLookback     *int   `json:"probeLookback"`
		DefaultCredential *int64 `json:"defaultCredential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	ctx, u := r.Context(), userOf(r)
	for _, f := range []struct {
		val      *int
		key      string
		min, max int
		mult     int
	}{
		{body.IntervalMinutes, "collection_interval_seconds", 1, 10080, 60},
		{body.JitterSeconds, "collection_jitter_seconds", 0, 3600, 1},
		{body.SSHWorkers, "ssh_workers", 1, 64, 1},
		{body.CommandTimeout, "ssh_command_timeout_seconds", 5, 600, 1},
		{body.MaxFiles, "snapshot_max_files", 10, 100000, 1},
		{body.ProbeRedirects, "probe_max_redirects", 0, 20, 1},
		{body.ProbeLookback, "probe_log_lookback_seconds", 10, 3600, 1},
	} {
		if f.val == nil {
			continue
		}
		if *f.val < f.min || *f.val > f.max {
			apiError(w, http.StatusUnprocessableEntity, "invalid_request", f.key+" is outside its allowed range.")
			return
		}
		if err := s.DB.SetSetting(ctx, f.key, strconv.Itoa(*f.val*f.mult), &u.ID); err != nil {
			apiError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
	}
	if body.DefaultCredential != nil {
		if *body.DefaultCredential < 0 {
			apiError(w, http.StatusUnprocessableEntity, "invalid_request", "Default credential is invalid.")
			return
		}
		if err := s.DB.SetSetting(ctx, "default_credential_id", strconv.FormatInt(*body.DefaultCredential, 10), &u.ID); err != nil {
			apiError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
	}
	_ = s.DB.Audit(ctx, &u.ID, "collection.defaults.update", "setting", nil, "collection defaults")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- users

func (s *Server) getUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.Users(r.Context())
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.UsersResponse{}
	for _, u := range list {
		item := &pb.UserItem{Id: u.ID, Username: u.Username, Role: u.Role, DisplayName: u.DisplayName,
			MustChangePassword: u.MustChangePassword, CreatedAt: u.CreatedAt, Disabled: u.Disabled}
		if u.LastLoginAt.Valid {
			item.LastLoginAt = u.LastLoginAt.String
		}
		resp.Users = append(resp.Users, item)
	}
	writeProto(w, http.StatusOK, resp)
}

func (s *Server) postAddUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		Confirm    string `json:"confirm"`
		Role       string `json:"role"`
		MustChange bool   `json:"mustChange"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	if body.Username == "" || len(body.Password) < 12 {
		apiError(w, http.StatusUnprocessableEntity, "invalid_request",
			"A username and a password of at least 12 characters are required.")
		return
	}
	if body.Password != body.Confirm {
		apiError(w, http.StatusUnprocessableEntity, "invalid_request", "The two passwords do not match.")
		return
	}
	if body.Role != "admin" && body.Role != "viewer" {
		apiError(w, http.StatusUnprocessableEntity, "invalid_request", "Role must be admin or viewer.")
		return
	}
	ctx, actor := r.Context(), userOf(r)
	id, err := s.DB.CreateUser(ctx, body.Username, body.Password, body.Role, body.Username, body.MustChange)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "create_failed", err.Error())
		return
	}
	_ = s.DB.Audit(ctx, &actor.ID, "user.create", "user", &id, body.Username)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// postSetUserDisabled returns a handler for both the disable and enable
// routes — the only difference between them is the boolean they set, so one
// closure covers both rather than two near-identical functions.
func (s *Server) postSetUserDisabled(disabled bool) http.HandlerFunc {
	action, word := "user.enable", "enabled"
	if disabled {
		action, word = "user.disable", "disabled"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, actor := r.Context(), userOf(r)
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid_id", "Invalid user id.")
			return
		}
		target, err := s.DB.User(ctx, id)
		if err != nil {
			apiError(w, http.StatusNotFound, "not_found", "User not found.")
			return
		}
		if disabled && target.IsAdmin() && !target.Disabled {
			n, err := s.DB.EnabledAdminCount(ctx, id)
			if err != nil {
				apiError(w, http.StatusInternalServerError, "update_failed", err.Error())
				return
			}
			if n == 0 {
				apiError(w, http.StatusUnprocessableEntity, "last_admin", "Cannot disable the last enabled admin account.")
				return
			}
		}
		if err := s.DB.SetUserDisabled(ctx, id, disabled); err != nil {
			apiError(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
		_ = s.DB.Audit(ctx, &actor.ID, action, "user", &id, target.Username)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": word})
	}
}

// ---------------------------------------------------------------- audit

func (s *Server) getAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	filter := store.AuditFilter{
		Actor:      q.Get("actor"),
		Action:     q.Get("action"),
		DateRange:  q.Get("range"),
		Page:       1,
		PerPage:    50,
		ExportMode: q.Get("export") == "csv",
	}
	if filter.DateRange == "" {
		filter.DateRange = "7d"
	}
	if p, err := strconv.Atoi(q.Get("page")); err == nil && p > 0 {
		filter.Page = p
	}
	if pp, err := strconv.Atoi(q.Get("per_page")); err == nil && pp > 0 && pp <= 200 {
		filter.PerPage = pp
	}

	result, err := s.DB.AuditEventsFiltered(ctx, filter)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}

	if filter.ExportMode {
		rows := [][]string{{"at", "actor", "action", "target_kind", "target", "outcome", "detail", "source_ip"}}
		for _, e := range result.Events {
			rows = append(rows, []string{e.At, e.ActorLabel, e.Action, e.TargetKind, e.TargetLabel, e.Outcome, e.Detail, e.SourceIP})
		}
		s.writeCSV(w, r, "audit", rows)
		return
	}

	actions, err := s.DB.AuditActions(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.AuditResponse{Total: int32(result.Total), TotalPages: int32(result.TotalPages),
		Page: int32(result.Page), PerPage: int32(result.PerPage), Actions: actions}
	for _, e := range result.Events {
		resp.Events = append(resp.Events, &pb.AuditEventItem{At: e.At, ActorLabel: e.ActorLabel, Action: e.Action,
			TargetKind: e.TargetKind, TargetLabel: e.TargetLabel, Outcome: e.Outcome, Detail: e.Detail, SourceIp: e.SourceIP})
	}
	writeProto(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------- api keys

func (s *Server) getAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.DB.APITokensFiltered(r.Context(), r.URL.Query().Get("state"))
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.ApiKeysResponse{}
	for _, k := range keys {
		item := &pb.ApiKeyItem{Id: k.ID, Name: k.Name, Username: k.Username, CreatedAt: k.CreatedAt, Prefix: k.Prefix}
		if k.LastUsedAt.Valid {
			item.LastUsedAt = k.LastUsedAt.String
		}
		if k.ExpiresAt.Valid {
			item.ExpiresAt = k.ExpiresAt.String
		}
		if k.RevokedAt.Valid {
			item.RevokedAt = k.RevokedAt.String
		}
		resp.Keys = append(resp.Keys, item)
	}
	writeProto(w, http.StatusOK, resp)
}

func (s *Server) postCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		ExpiresDays int    `json:"expiresDays"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	if body.Name == "" {
		apiError(w, http.StatusUnprocessableEntity, "invalid_request", "A key name is required.")
		return
	}
	if body.ExpiresDays < 1 || body.ExpiresDays > 3650 {
		apiError(w, http.StatusUnprocessableEntity, "invalid_request", "Expiry must be between 1 and 3650 days.")
		return
	}
	ctx, actor := r.Context(), userOf(r)
	id, token, err := s.DB.CreateAPIToken(ctx, body.Name, actor.ID, time.Duration(body.ExpiresDays)*24*time.Hour)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "create_failed", err.Error())
		return
	}
	_ = s.DB.Audit(ctx, &actor.ID, "api_token.create", "api_token", &id, body.Name)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "token": token})
}

func (s *Server) postRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_id", "Invalid API key id.")
		return
	}
	ctx, actor := r.Context(), userOf(r)
	if err := s.DB.RevokeAPIToken(ctx, id); err != nil {
		apiError(w, http.StatusNotFound, "not_found", "API key not found or already revoked.")
		return
	}
	_ = s.DB.Audit(ctx, &actor.ID, "api_token.revoke", "api_token", &id, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- license

func (s *Server) getLicense(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lic := s.CurrentLicense()
	status, message := s.LicenseStatus(ctx)
	n, err := s.DB.NodeCount(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	state, err := s.DB.LicenseState(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp := &pb.LicenseResponse{Status: string(status), Message: message, NodeCount: int32(n)}
	if lic != nil {
		resp.Loaded = true
		resp.Customer, resp.Edition, resp.NodeCeiling = lic.Customer, lic.Edition, int32(lic.NodeCeiling)
		resp.Expiry = lic.Expiry.Format(time.RFC3339)
	}
	if state != nil {
		resp.State = &pb.LicenseStatePB{Customer: state.Customer, Edition: state.Edition,
			NodeCeiling: int32(state.NodeCeiling), ExpiresAt: state.ExpiresAt,
			SignatureValid: state.SignatureValid, LastEvaluatedAt: state.LastEvaluatedAt,
			InstalledByUsername: state.InstalledByUsername}
	}
	writeProto(w, http.StatusOK, resp)
}

func (s *Server) postInstallLicense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LicenseText string `json:"licenseText"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apiError(w, http.StatusBadRequest, "invalid_body", "Could not parse request body.")
		return
	}
	ctx, u := r.Context(), userOf(r)
	lic, err := s.InstallLicense(ctx, []byte(body.LicenseText), &u.ID)
	if err != nil {
		apiError(w, http.StatusUnprocessableEntity, "install_failed", err.Error())
		return
	}
	// Customer name only — never the raw license text or its signature.
	_ = s.DB.Audit(ctx, &u.ID, "license.install", "license", nil, lic.Customer)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---------------------------------------------------------------- system / diagnostics

func (s *Server) getDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	started, listenAddr, tlsEnabled := s.Diag()
	resp := &pb.DiagnosticsResponse{
		Version: Version, GoVersion: runtime.Version(), UptimeSeconds: int64(time.Since(started).Round(time.Second).Seconds()),
		DbPath: s.DB.Path, ListenAddr: listenAddr, TlsEnabled: tlsEnabled, DemoMode: s.DemoMode,
		MasterKeyPath: s.Master.Path,
	}
	if fi, err := os.Stat(s.DB.Path); err == nil {
		resp.DbSizeBytes = fi.Size()
	}
	if fi, err := os.Stat(s.Master.Path); err == nil {
		resp.MasterKeyPresent = true
		resp.MasterKeyMode = fmt.Sprintf("%#o", fi.Mode().Perm())
	}
	status, message := s.LicenseStatus(ctx)
	resp.LicenseStatus, resp.LicenseMessage = string(status), message
	applied, err := s.DB.AppliedMigrations(ctx)
	if err != nil {
		apiError(w, http.StatusServiceUnavailable, "datastore_unavailable", err.Error())
		return
	}
	resp.MigrationsApplied = int32(len(applied))
	expected, _ := store.ExpectedMigrationCount()
	resp.MigrationsExpected = int32(expected)
	writeProto(w, http.StatusOK, resp)
}
