package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nagiflow/nagipath/internal/keys"
)

// A throwaway ed25519 key, generated for this test only.
const rekeyTestKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBKjOmntBSro1ndD+eIwP42a+NMIblJQSqvNpY9tPR4AgAAAJi1g/r/tYP6
/wAAAAtzc2gtZWQyNTUxOQAAACBKjOmntBSro1ndD+eIwP42a+NMIblJQSqvNpY9tPR4Ag
AAAEB2ROXtat6XEeEl08vk8V8C4iFKTFkxxurJfZOucR9ZTkqM6ae0FKujWd0P54jA/jZr
40whuUlBKq82lj209HgCAAAAEW5hZ2lwYXRoIHRlc3Qga2V5AQIDBA==
-----END OPENSSH PRIVATE KEY-----
`

func masterAt(t *testing.T, path string) *keys.Master {
	t.Helper()
	if err := keys.Generate(path); err != nil {
		t.Fatal(err)
	}
	m, err := keys.Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Rotating the Master Key has to leave every stored credential usable under the
// new key and unusable under the old one. Both halves matter: a rekey that
// silently left the old ciphertext in place would look like a success and would
// mean the leaked key still opens the database.
func TestRekeyMovesEveryCredentialToTheNewKey(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "rekey.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	oldKey := masterAt(t, filepath.Join(dir, "old.key"))
	newKey := masterAt(t, filepath.Join(dir, "new.key"))

	// Two credentials, so the loop is a loop.
	var ids []int64
	for _, name := range []string{"fleet-readonly", "legacy"} {
		id, err := db.CreateCredential(ctx, oldKey, name, "nagipath", "private_key", rekeyTestKey, "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	n, err := db.Rekey(ctx, oldKey, newKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(ids) {
		t.Errorf("rekey reported %d credentials, want %d", n, len(ids))
	}

	for _, id := range ids {
		if _, _, err := db.Signer(ctx, newKey, id); err != nil {
			t.Errorf("credential %d does not open under the new key: %v", id, err)
		}
		if _, _, err := db.Signer(ctx, oldKey, id); err == nil {
			t.Errorf("credential %d still opens under the old key", id)
		}
	}

	// The wrong -old key must fail before it writes anything, or a mistyped path
	// would half-convert the database into one no single key can open.
	third := masterAt(t, filepath.Join(dir, "third.key"))
	if _, err := db.Rekey(ctx, third, oldKey); err == nil {
		t.Fatal("rekey with the wrong old key succeeded")
	} else if !strings.Contains(err.Error(), "--old") {
		t.Errorf("the error does not name the likely cause: %v", err)
	}
	for _, id := range ids {
		if _, _, err := db.Signer(ctx, newKey, id); err != nil {
			t.Errorf("credential %d was damaged by the failed rekey: %v", id, err)
		}
	}
}
