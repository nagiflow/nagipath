package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAPITokenLifecycle(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "tokens.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	userID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}
	id, raw, err := db.CreateAPIToken(ctx, "ci", userID, 90*24*time.Hour)
	if err != nil || raw == "" {
		t.Fatalf("CreateAPIToken = %d, %q, %v", id, raw, err)
	}
	if u, err := db.AuthenticateAPIToken(ctx, raw); err != nil || u.ID != userID {
		t.Fatalf("AuthenticateAPIToken = %+v, %v", u, err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, raw+"wrong"); err == nil {
		t.Fatal("a wrong token authenticated")
	}
	if err := db.RevokeAPIToken(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AuthenticateAPIToken(ctx, raw); err == nil {
		t.Fatal("a revoked token authenticated")
	}
}
