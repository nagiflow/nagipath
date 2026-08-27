package web

import (
	"net/http"
	"strings"

	"github.com/nagiflow/nagipath/internal/store"
)

// ---------------------------------------------------------------- users

type usersData struct {
	Users                      []store.User
	SnapshotDays, JobLogDays   int
	ProbeDays, AuditDays       int
	SSHWorkers, CommandTimeout int
	CollectionIntervalMinutes  int
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.DB.Users(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d := usersData{
		Users:                     list,
		SnapshotDays:              s.DB.SettingInt(ctx, "snapshot_retention_days"),
		JobLogDays:                s.DB.SettingInt(ctx, "job_log_retention_days"),
		ProbeDays:                 s.DB.SettingInt(ctx, "probe_retention_days"),
		AuditDays:                 s.DB.SettingInt(ctx, "audit_retention_days"),
		SSHWorkers:                s.DB.SettingInt(ctx, "ssh_workers"),
		CommandTimeout:            s.DB.SettingInt(ctx, "ssh_command_timeout_seconds"),
		CollectionIntervalMinutes: s.DB.SettingInt(ctx, "collection_interval_seconds") / 60,
	}
	s.render(w, r, "users.html", "Settings", d)
}

func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || len(password) < 12 {
		redirect(w, r, "/users", "", "a username and a password of at least 12 characters are required")
		return
	}
	if password != r.FormValue("confirm") {
		redirect(w, r, "/users", "", "the two passwords do not match")
		return
	}
	role := r.FormValue("role")
	if role != "admin" && role != "viewer" {
		redirect(w, r, "/users", "", "role must be admin or viewer")
		return
	}
	mustChange := r.FormValue("must_change") != ""
	id, err := s.DB.CreateUser(ctx, username, password, role, username, mustChange)
	if err != nil {
		redirect(w, r, "/users", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &actor.ID, "user.create", "user", &id, username)
	redirect(w, r, "/users", "user created", "")
}

// disableUser refuses to leave the product with zero enabled admins — the same
// class of structural guardrail as Probe's GET/HEAD-only, enforced here rather
// than only by hiding the button, because a button that is not rendered is not
// a guarantee.
func (s *Server) disableUser(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	id := idOf(r, "id")
	target, err := s.DB.User(ctx, id)
	if err != nil {
		redirect(w, r, "/users", "", "user not found")
		return
	}
	if target.IsAdmin() && !target.Disabled {
		n, err := s.DB.EnabledAdminCount(ctx, id)
		if err != nil {
			redirect(w, r, "/users", "", err.Error())
			return
		}
		if n == 0 {
			redirect(w, r, "/users", "", "cannot disable the last enabled admin account")
			return
		}
	}
	if err := s.DB.SetUserDisabled(ctx, id, true); err != nil {
		redirect(w, r, "/users", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &actor.ID, "user.disable", "user", &id, target.Username)
	redirect(w, r, "/users", "user disabled", "")
}

func (s *Server) enableUser(w http.ResponseWriter, r *http.Request) {
	ctx, actor := r.Context(), userOf(r)
	id := idOf(r, "id")
	target, err := s.DB.User(ctx, id)
	if err != nil {
		redirect(w, r, "/users", "", "user not found")
		return
	}
	if err := s.DB.SetUserDisabled(ctx, id, false); err != nil {
		redirect(w, r, "/users", "", err.Error())
		return
	}
	s.DB.Audit(ctx, &actor.ID, "user.enable", "user", &id, target.Username)
	redirect(w, r, "/users", "user enabled", "")
}
