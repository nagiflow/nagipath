package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/store"
)

// Settings index and its subsection pages. The index is aspirational in the
// wireframe but has no content of its own — it would just list the sub-nav
// again, so it redirects to the first section instead.

func (s *Server) settingsIndex(w http.ResponseWriter, r *http.Request) {
	// Viewers cannot open the admin-only landing page, so they land on the audit
	// trail they can read.
	if !userOf(r).IsAdmin() {
		http.Redirect(w, r, "/settings/audit", http.StatusSeeOther)
		return
	}
	// Admin sees the full Settings landing page: Users table plus Retention and
	// Collection defaults summaries.
	data, err := s.DB.SettingsView(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, "settings.html", "Settings", data)
}

// hostKeys is the Settings page for Host Key Approval. Until a Node's key is
// approved, nagipath runs no command on it — no collection, no effective config
// read, nothing. Shows all keys (pending, approved, changed) with filters.
func (s *Server) hostKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stateFilter := r.URL.Query().Get("state")
	clusterFilter := r.URL.Query().Get("cluster")

	stats, err := s.DB.HostKeyStats(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	keys, err := s.DB.AllHostKeys(ctx, stateFilter, clusterFilter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := struct {
		Stats         store.HostKeyStats
		Keys          []store.PendingHostKey
		StateFilter   string
		ClusterFilter string
	}{stats, keys, stateFilter, clusterFilter}
	s.render(w, r, "hostkeys.html", "Host keys", data)
}

// masterKey is the Settings page explaining where the Master Key lives and how
// to rotate it. The key itself is never rendered — it is read from disk on
// startup and held in memory, never stored in the database.
func (s *Server) masterKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := s.DB.MasterKeyEncryptedCounts(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := struct {
		Path    string
		DataDir string
		Counts  store.MasterKeyEncryptedCounts
	}{Path: s.Master.Path, Counts: counts}
	if filepath.IsAbs(s.Master.Path) {
		data.DataDir = filepath.Dir(s.Master.Path)
	}
	// No MISSING state: the server refuses to start without the Master Key
	// (ADR-0011), so a request reaching this handler is itself the proof it loaded.
	s.render(w, r, "masterkey.html", "Master key", data)
}

// retention is the Settings page for data retention policies. Snapshots, job
// logs, probe records and audit log entries are all kept for a configured
// number of days, with one exception: a Snapshot that recorded a change is kept
// beyond its window so drift history does not vanish.
//
// The numbers here were hardcoded and enforced by nothing, which is the worst
// version of this screen: it told an operator their audit trail was pruned at
// 400 days while it grew forever. They are settings now, and store.Prune reads
// the same keys.
func (s *Server) retention(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := s.DB.RetentionStats(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := struct {
		SnapshotDays   int
		MinPerInstance int
		JobLogDays     int
		ProbeDays      int
		AuditDays      int
		Stats          store.RetentionStats
	}{
		SnapshotDays:   s.DB.SettingInt(ctx, "snapshot_retention_days"),
		MinPerInstance: s.DB.SettingInt(ctx, "snapshot_retention_min_per_instance"),
		JobLogDays:     s.DB.SettingInt(ctx, "job_log_retention_days"),
		ProbeDays:      s.DB.SettingInt(ctx, "probe_retention_days"),
		AuditDays:      s.DB.SettingInt(ctx, "audit_retention_days"),
		Stats:          stats,
	}
	s.render(w, r, "retention.html", "Retention", data)
}

// setRetention writes the four windows and the per-instance floor. Every field
// is validated here rather than trusted from the form: a negative window would
// make every cutoff a date in the future and prune the whole table on the next
// hourly pass.
func (s *Server) setRetention(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	const dest = "/settings/retention"
	for _, f := range []struct {
		field, key string
		max        int
	}{
		{"snapshot_days", "snapshot_retention_days", 3650},
		{"min_per_instance", "snapshot_retention_min_per_instance", 1000},
		{"job_log_days", "job_log_retention_days", 3650},
		{"probe_days", "probe_retention_days", 3650},
		{"audit_days", "audit_retention_days", 3650},
	} {
		raw := strings.TrimSpace(r.FormValue(f.field))
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > f.max {
			redirect(w, r, dest, "", "each window must be a whole number of days between 0 and "+strconv.Itoa(f.max))
			return
		}
		if err := s.DB.SetSetting(ctx, f.key, strconv.Itoa(n), &actor.ID); err != nil {
			redirect(w, r, dest, "", err.Error())
			return
		}
	}
	// Audited because it changes how long the audit trail itself is kept.
	s.DB.Audit(ctx, &actor.ID, "retention.update", "setting", nil, "retention windows")
	redirect(w, r, dest, "retention updated", "")
}

func (s *Server) runRetention(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	startTime := time.Now()
	result, err := s.DB.Prune(ctx)
	if err != nil {
		redirect(w, r, "/settings/retention", "", err.Error())
		return
	}
	duration := time.Since(startTime).Seconds()

	// Persist prune summary for the retention page
	history := store.PruneHistory{
		RanAt:    time.Now().UTC().Format(time.RFC3339),
		Duration: duration,
		Deleted:  result,
		// Examined and Kept would need counts before deletion; omitting for now as not in current Prune return
	}
	_ = s.DB.SavePruneHistory(ctx, history, &actor.ID)
	_ = s.DB.Audit(ctx, &actor.ID, "retention.run", "setting", nil, "manual prune")
	redirect(w, r, "/settings/retention", fmt.Sprintf(
		"pruned %d snapshots, %d job logs, %d probes, %d audit events and %d blobs",
		result.Snapshots, result.JobLogs, result.Probes, result.AuditEvents, result.Blobs), "")
}

type collectionDefaultsData struct {
	Credentials       []store.Credential
	DefaultCredential int64
	IntervalMinutes   int
	JitterSeconds     int
	SSHWorkers        int
	CommandTimeout    int
	MaxFiles          int
	ProbeRedirects    int
	ProbeLookback     int
}

func (s *Server) collectionDefaults(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	creds, err := s.DB.Credentials(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defaultCredential, _ := strconv.ParseInt(s.DB.Setting(ctx, "default_credential_id"), 10, 64)
	d := collectionDefaultsData{
		Credentials: creds, DefaultCredential: defaultCredential,
		IntervalMinutes: s.DB.SettingInt(ctx, "collection_interval_seconds") / 60,
		JitterSeconds:   s.DB.SettingInt(ctx, "collection_jitter_seconds"),
		SSHWorkers:      s.DB.SettingInt(ctx, "ssh_workers"),
		CommandTimeout:  s.DB.SettingInt(ctx, "ssh_command_timeout_seconds"),
		MaxFiles:        s.DB.SettingInt(ctx, "snapshot_max_files"),
		ProbeRedirects:  s.DB.SettingInt(ctx, "probe_max_redirects"),
		ProbeLookback:   s.DB.SettingInt(ctx, "probe_log_lookback_seconds"),
	}
	s.render(w, r, "collection_defaults.html", "Collection defaults", d)
}

func (s *Server) setCollectionDefaults(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	const dest = "/settings/collection-defaults"
	fields := []struct {
		form, key string
		min, max  int
		mult      int
	}{
		{"interval_minutes", "collection_interval_seconds", 1, 10080, 60},
		{"jitter_seconds", "collection_jitter_seconds", 0, 3600, 1},
		{"ssh_workers", "ssh_workers", 1, 64, 1},
		{"command_timeout", "ssh_command_timeout_seconds", 5, 600, 1},
		{"max_files", "snapshot_max_files", 10, 100000, 1},
		{"probe_redirects", "probe_max_redirects", 0, 20, 1},
		{"probe_lookback", "probe_log_lookback_seconds", 10, 3600, 1},
	}
	for _, field := range fields {
		n, err := strconv.Atoi(strings.TrimSpace(r.FormValue(field.form)))
		if err != nil || n < field.min || n > field.max {
			redirect(w, r, dest, "", field.form+" is outside its allowed range")
			return
		}
		if err := s.DB.SetSetting(ctx, field.key, strconv.Itoa(n*field.mult), &actor.ID); err != nil {
			redirect(w, r, dest, "", err.Error())
			return
		}
	}
	credential := strings.TrimSpace(r.FormValue("default_credential"))
	if credential != "" {
		id, err := strconv.ParseInt(credential, 10, 64)
		if err != nil || id < 0 {
			redirect(w, r, dest, "", "default credential is invalid")
			return
		}
		if err := s.DB.SetSetting(ctx, "default_credential_id", strconv.FormatInt(id, 10), &actor.ID); err != nil {
			redirect(w, r, dest, "", err.Error())
			return
		}
	}
	_ = s.DB.Audit(ctx, &actor.ID, "collection.defaults.update", "setting", nil, "collection defaults")
	redirect(w, r, dest, "collection defaults saved", "")
}

func (s *Server) apiKeys(w http.ResponseWriter, r *http.Request) {
	s.renderAPIKeys(w, r, "")
}

func (s *Server) renderAPIKeys(w http.ResponseWriter, r *http.Request, created string) {
	stateFilter := r.URL.Query().Get("state")
	keys, err := s.DB.APITokensFiltered(r.Context(), stateFilter)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := struct {
		Keys         []store.APIToken
		CreatedToken string
		StateFilter  string
	}{keys, created, stateFilter}
	s.render(w, r, "api_keys.html", "API keys", data)
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		redirect(w, r, "/settings/api-keys", "", "a key name is required")
		return
	}
	days, err := strconv.Atoi(r.FormValue("expires_days"))
	if err != nil || days < 1 || days > 3650 {
		redirect(w, r, "/settings/api-keys", "", "expiry must be between 1 and 3650 days")
		return
	}
	id, token, err := s.DB.CreateAPIToken(ctx, name, actor.ID, time.Duration(days)*24*time.Hour)
	if err != nil {
		redirect(w, r, "/settings/api-keys", "", err.Error())
		return
	}
	_ = s.DB.Audit(ctx, &actor.ID, "api_token.create", "api_token", &id, name)
	s.renderAPIKeys(w, r, token)
}

func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	id := idOf(r, "id")
	if err := s.DB.RevokeAPIToken(ctx, id); err != nil {
		redirect(w, r, "/settings/api-keys", "", "API key not found or already revoked")
		return
	}
	_ = s.DB.Audit(ctx, &actor.ID, "api_token.revoke", "api_token", &id, "")
	redirect(w, r, "/settings/api-keys", "API key revoked", "")
}
