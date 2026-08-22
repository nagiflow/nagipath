package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func userTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "user.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPasswordAndSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	db := userTestDB(t)

	id, err := db.CreateUser(ctx, "admin", "correct horse battery", "admin", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Authenticate(ctx, "admin", "wrong"); !errors.Is(err, ErrAuth) {
		t.Errorf("wrong password: err = %v, want ErrAuth", err)
	}
	if _, err := db.Authenticate(ctx, "nobody", "correct horse battery"); !errors.Is(err, ErrAuth) {
		t.Errorf("unknown user: err = %v, want ErrAuth", err)
	}
	u, err := db.Authenticate(ctx, "admin", "correct horse battery")
	if err != nil {
		t.Fatalf("correct password rejected: %v", err)
	}
	if !u.MustChangePassword {
		t.Error("a bootstrap user must be forced to change its password")
	}

	token, err := db.NewSession(ctx, id, "test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	// The raw token must not be recoverable from the database.
	var stored string
	db.R.QueryRowContext(ctx, `SELECT token_hash FROM user_session`).Scan(&stored)
	if stored == token || stored == "" {
		t.Errorf("session token stored as %q; only its hash may be persisted", stored)
	}

	if got, err := db.SessionUser(ctx, token); err != nil || got.ID != id {
		t.Errorf("session lookup = %+v, %v", got, err)
	}
	if _, err := db.SessionUser(ctx, "not-a-token"); err == nil {
		t.Error("a forged token was accepted")
	}

	// A disabled account cannot log in and its live sessions stop working.
	if err := db.SetUserDisabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser(ctx, token); err == nil {
		t.Error("a disabled user kept a working session")
	}
	if _, err := db.Authenticate(ctx, "admin", "correct horse battery"); !errors.Is(err, ErrAuth) {
		t.Error("a disabled user was able to authenticate")
	}

	if err := db.SetUserDisabled(ctx, id, false); err != nil {
		t.Fatal(err)
	}
	if err := db.SetPassword(ctx, id, "a whole new password"); err != nil {
		t.Fatal(err)
	}
	if err := db.EndAllSessions(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser(ctx, token); err == nil {
		t.Error("changing the password left an old session valid")
	}
	if u, err := db.Authenticate(ctx, "admin", "a whole new password"); err != nil {
		t.Errorf("new password rejected: %v", err)
	} else if u.MustChangePassword {
		t.Error("setting a password should clear must_change_password")
	}
}

// The last-admin guardrail is a count, not a flag, so it has to stay correct as
// admins are added, disabled and re-enabled around it.
func TestEnabledAdminCount(t *testing.T) {
	ctx := context.Background()
	db := userTestDB(t)

	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	// The lone admin, excluding itself, counts zero others.
	if n, err := db.EnabledAdminCount(ctx, adminID); err != nil || n != 0 {
		t.Fatalf("EnabledAdminCount excluding the only admin = %d, %v; want 0, nil", n, err)
	}

	secondID, err := db.CreateUser(ctx, "admin2", "a good long password", "admin", "Admin2", false)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := db.EnabledAdminCount(ctx, adminID); err != nil || n != 1 {
		t.Fatalf("EnabledAdminCount with a second admin = %d, %v; want 1, nil", n, err)
	}
	// A viewer never counts, no matter how many exist.
	if _, err := db.CreateUser(ctx, "viewer", "a good long password", "viewer", "Viewer", false); err != nil {
		t.Fatal(err)
	}
	if n, err := db.EnabledAdminCount(ctx, adminID); err != nil || n != 1 {
		t.Fatalf("EnabledAdminCount counted a viewer: %d, %v", n, err)
	}
	// A disabled admin does not count as an enabled one.
	if err := db.SetUserDisabled(ctx, secondID, true); err != nil {
		t.Fatal(err)
	}
	if n, err := db.EnabledAdminCount(ctx, adminID); err != nil || n != 0 {
		t.Fatalf("EnabledAdminCount counted a disabled admin: %d, %v", n, err)
	}
}

// Login rate limiting counts the failure audit_events postLogin already writes,
// which is what makes it a query rather than a new table.
func TestLoginRateLimitedCountsFailuresWithinTheWindow(t *testing.T) {
	ctx := context.Background()
	db := userTestDB(t)

	if err := db.SetSetting(ctx, "login_rate_limit_max", "2", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(ctx, "login_rate_limit_window_seconds", "60", nil); err != nil {
		t.Fatal(err)
	}

	fail := func(username string, age time.Duration) {
		t.Helper()
		when := time.Now().UTC().Add(-age).Format("2006-01-02T15:04:05Z")
		if _, err := db.W.ExecContext(ctx, `INSERT INTO audit_event
			(at, actor_label, action, target_kind, target_label, outcome)
			VALUES (?,?,?,?,?,?)`, when, "user", "auth.login", "user", username, "failure"); err != nil {
			t.Fatal(err)
		}
	}

	if limited, err := db.LoginRateLimited(ctx, "admin"); err != nil || limited {
		t.Fatalf("LoginRateLimited with no history = %v, %v; want false, nil", limited, err)
	}

	fail("admin", 30*time.Second)
	fail("admin", 10*time.Second)
	if limited, err := db.LoginRateLimited(ctx, "admin"); err != nil || !limited {
		t.Fatalf("LoginRateLimited at the ceiling = %v, %v; want true, nil", limited, err)
	}
	// A different username is a different bucket.
	if limited, err := db.LoginRateLimited(ctx, "other"); err != nil || limited {
		t.Errorf("the limit leaked across usernames: %v, %v", limited, err)
	}

	// Age the failures past the window: the guardrail lifts on its own.
	if _, err := db.W.ExecContext(ctx, `UPDATE audit_event SET at = ? WHERE target_label = 'admin'`,
		time.Now().UTC().Add(-90*time.Second).Format("2006-01-02T15:04:05Z")); err != nil {
		t.Fatal(err)
	}
	if limited, err := db.LoginRateLimited(ctx, "admin"); err != nil || limited {
		t.Errorf("LoginRateLimited after the window passed = %v, %v; want false, nil", limited, err)
	}

	// login_rate_limit_max = 0 disables the guardrail rather than blocking everything.
	if err := db.SetSetting(ctx, "login_rate_limit_max", "0", nil); err != nil {
		t.Fatal(err)
	}
	fail("admin", time.Second)
	fail("admin", time.Second)
	fail("admin", time.Second)
	if limited, err := db.LoginRateLimited(ctx, "admin"); err != nil || limited {
		t.Errorf("login_rate_limit_max=0 = %v, %v; want the guardrail disabled (false, nil)", limited, err)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$broken$x$y",
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5"} {
		if VerifyPassword(bad, "anything") {
			t.Errorf("malformed hash %q verified as valid", bad)
		}
	}
}
