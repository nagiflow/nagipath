package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
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

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$broken$x$y",
		"$bcrypt$v=19$m=1,t=1,p=1$c2FsdA$a2V5"} {
		if VerifyPassword(bad, "anything") {
			t.Errorf("malformed hash %q verified as valid", bad)
		}
	}
}
