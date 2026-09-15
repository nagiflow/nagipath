package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCheckHostKeyTofu(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "tofu.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()

	nodeID, err := db.AddNode(ctx, "10.0.0.1", 22, "n1", "root", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}

	// tofu_enabled off (default): an unrecognized key is refused and left pending.
	if err := db.CheckHostKey(ctx, nodeID, "ed25519", "ssh-ed25519 AAAAfirst", "SHA256:first"); !errors.As(err, new(*ErrHostKeyPending)) {
		t.Fatalf("CheckHostKey with tofu off = %v, want ErrHostKeyPending", err)
	}
	keys, err := db.HostKeys(ctx, nodeID)
	if err != nil || len(keys) != 1 || keys[0].State != "pending" {
		t.Fatalf("HostKeys = %+v, %v; want one pending row", keys, err)
	}

	// tofu_enabled on: a different node's first-ever key is auto-approved.
	if err := db.SetSetting(ctx, "tofu_enabled", "1", nil); err != nil {
		t.Fatal(err)
	}
	node2ID, err := db.AddNode(ctx, "10.0.0.2", 22, "n2", "root", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CheckHostKey(ctx, node2ID, "ed25519", "ssh-ed25519 AAAAsecond", "SHA256:second"); err != nil {
		t.Fatalf("CheckHostKey with tofu on, first sighting = %v, want nil", err)
	}
	keys, err = db.HostKeys(ctx, node2ID)
	if err != nil || len(keys) != 1 || keys[0].State != "approved" {
		t.Fatalf("HostKeys = %+v, %v; want one approved row", keys, err)
	}

	// tofu_enabled on: a key that would replace the now-approved one is still
	// a mismatch, never auto-approved.
	err = db.CheckHostKey(ctx, node2ID, "ed25519", "ssh-ed25519 AAAAreplacement", "SHA256:replacement")
	if !errors.As(err, new(*ErrHostKeyMismatch)) {
		t.Fatalf("CheckHostKey on a rekey with tofu on = %v, want ErrHostKeyMismatch", err)
	}
	keys, err = db.HostKeys(ctx, node2ID)
	if err != nil || len(keys) != 2 {
		t.Fatalf("HostKeys after rekey = %+v, %v; want the approved row plus one pending", keys, err)
	}
}
