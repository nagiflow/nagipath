package store

import (
	"context"
	"path/filepath"
	"testing"
)

// The one rule of editing a credential that is not obvious from the call:
// a blank secret keeps what is sealed. The browser is never sent the stored
// password, so a form that submits "" must not wipe it — otherwise fixing a
// typo in the username silently destroys the credential.
func TestUpdateCredentialKeepsTheSealedPasswordWhenBlank(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "cred.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	m := masterAt(t, filepath.Join(dir, "master.key"))

	id, err := db.CreatePasswordCredential(ctx, m, "ops", "svc-nagipath", "username_password", "hunter2", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.UpdateCredential(ctx, m, id, "ops", "svc-collect", "", "", "", "", ""); err != nil {
		t.Fatal(err)
	}
	user, password, err := db.SSHPassword(ctx, m, id)
	if err != nil {
		t.Fatal(err)
	}
	if user != "svc-collect" {
		t.Errorf("username = %q, want the edited one", user)
	}
	if password != "hunter2" {
		t.Errorf("password = %q, want the stored one kept by a blank field", password)
	}

	if err := db.UpdateCredential(ctx, m, id, "ops", "svc-collect", "", "", "", "correct-horse", ""); err != nil {
		t.Fatal(err)
	}
	if _, password, err = db.SSHPassword(ctx, m, id); err != nil || password != "correct-horse" {
		t.Errorf("password = %q (%v), want the new one", password, err)
	}
}
