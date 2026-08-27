package store

import (
	"context"
	"path/filepath"
	"testing"
)

// ponytail: the seventh copy of this in the package's tests. Worth folding all
// seven into one shared helper the next time one of them needs changing.
func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSettingsView(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	// Create a user
	userID, err := db.CreateUser(ctx, "testuser", "password123456", "admin", "Test User", false)
	if err != nil {
		t.Fatal(err)
	}

	// Set some retention values
	_ = db.SetSetting(ctx, "snapshot_retention_days", "90", &userID)
	_ = db.SetSetting(ctx, "ssh_workers", "8", &userID)
	_ = db.SetSetting(ctx, "collection_interval_seconds", "3600", &userID)

	data, err := db.SettingsView(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Users) == 0 {
		t.Error("expected at least one user")
	}
	if data.SnapshotDays != 90 {
		t.Errorf("expected SnapshotDays=90, got %d", data.SnapshotDays)
	}
	if data.SSHWorkers != 8 {
		t.Errorf("expected SSHWorkers=8, got %d", data.SSHWorkers)
	}
	if data.CollectionIntervalMinutes != 60 {
		t.Errorf("expected CollectionIntervalMinutes=60, got %d", data.CollectionIntervalMinutes)
	}
}

func TestHostKeyStats(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	nodeID, _ := db.AddNode(ctx, "192.0.2.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)

	// Insert pending keys
	_, _ = db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at)
		VALUES (?, 'ed25519', 'key1', 'SHA256:pending1', 'pending', datetime('now'))`, nodeID)
	_, _ = db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at, decided_at)
		VALUES (?, 'rsa', 'key2', 'SHA256:approved1', 'approved', datetime('now'), datetime('now'))`, nodeID)

	stats, err := db.HostKeyStats(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Pending != 1 {
		t.Errorf("expected Pending=1, got %d", stats.Pending)
	}
	if stats.Approved != 1 {
		t.Errorf("expected Approved=1, got %d", stats.Approved)
	}
	if len(stats.Algorithms) != 2 {
		t.Errorf("expected 2 algorithms, got %d", len(stats.Algorithms))
	}
	if stats.Algorithms["ed25519"] != 1 {
		t.Errorf("expected 1 ed25519 key, got %d", stats.Algorithms["ed25519"])
	}
	if stats.Algorithms["rsa"] != 1 {
		t.Errorf("expected 1 rsa key, got %d", stats.Algorithms["rsa"])
	}
}

func TestAllHostKeysFiltering(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	nodeID, _ := db.AddNode(ctx, "192.0.2.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)

	// Insert various keys
	_, _ = db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at)
		VALUES (?, 'ed25519', 'key1', 'SHA256:pending1', 'pending', datetime('now'))`, nodeID)
	_, _ = db.W.ExecContext(ctx, `INSERT INTO host_key
		(node_id, algorithm, public_key, fingerprint, state, first_seen_at, decided_at)
		VALUES (?, 'rsa', 'key2', 'SHA256:approved1', 'approved', datetime('now'), datetime('now'))`, nodeID)

	tests := []struct {
		stateFilter string
		want        int
	}{
		{"", 2},
		{"pending", 1},
		{"approved", 1},
	}

	for _, tt := range tests {
		keys, err := db.AllHostKeys(ctx, tt.stateFilter, "")
		if err != nil {
			t.Fatalf("filter=%s: %v", tt.stateFilter, err)
		}
		if len(keys) != tt.want {
			t.Errorf("filter=%s: expected %d keys, got %d", tt.stateFilter, tt.want, len(keys))
		}
	}
}

func TestMasterKeyEncryptedCounts(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	userID, _ := db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)

	// Create credentials and tokens. The count is of rows encrypted under the
	// master key, so the credentials have to go in through the real encrypting
	// path — a raw INSERT would be counted while holding no ciphertext.
	m := masterAt(t, filepath.Join(t.TempDir(), "master.key"))
	for _, name := range []string{"cred1", "cred2"} {
		if _, err := db.CreateCredential(ctx, m, name, "user", "private_key", rekeyTestKey, "", "", &userID); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _ = db.CreateAPIToken(ctx, "token1", userID, 0)

	counts, err := db.MasterKeyEncryptedCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if counts.Credentials != 2 {
		t.Errorf("expected Credentials=2, got %d", counts.Credentials)
	}
	if counts.APIKeys != 1 {
		t.Errorf("expected APIKeys=1, got %d", counts.APIKeys)
	}
}

func TestRetentionStats(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	nodeID, _ := db.AddNode(ctx, "192.0.2.1", 22, "test-node", "nagipath", nil, nil, "manual", nil)
	_, _ = db.StartCollection(ctx, nodeID, "manual", nil)

	stats, err := db.RetentionStats(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if stats.JobLogs < 1 {
		t.Error("expected at least 1 job log")
	}
}

func TestSavePruneHistory(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	userID, _ := db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)

	history := PruneHistory{
		RanAt:    "2024-08-26T10:00:00Z",
		Duration: 1.5,
		Deleted: Pruned{
			Snapshots: 10,
			JobLogs:   5,
		},
	}

	err := db.SavePruneHistory(ctx, history, &userID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it was saved
	stats, err := db.RetentionStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.LastPrune == nil {
		t.Fatal("expected LastPrune to be set")
	}
	if stats.LastPrune.Deleted.Snapshots != 10 {
		t.Errorf("expected 10 snapshots deleted, got %d", stats.LastPrune.Deleted.Snapshots)
	}
	if stats.LastPrune.Duration != 1.5 {
		t.Errorf("expected duration 1.5, got %f", stats.LastPrune.Duration)
	}
}

func TestAPITokensFiltered(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	userID, _ := db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)

	// Create active token
	_, _, _ = db.CreateAPIToken(ctx, "active-token", userID, 0)

	// Create and revoke a token
	revokedID, _, _ := db.CreateAPIToken(ctx, "revoked-token", userID, 0)
	_ = db.RevokeAPIToken(ctx, revokedID)

	tests := []struct {
		filter string
		want   int
	}{
		{"", 2},
		{"active", 1},
		{"revoked", 1},
	}

	for _, tt := range tests {
		tokens, err := db.APITokensFiltered(ctx, tt.filter)
		if err != nil {
			t.Fatalf("filter=%s: %v", tt.filter, err)
		}
		if len(tokens) != tt.want {
			t.Errorf("filter=%s: expected %d tokens, got %d", tt.filter, tt.want, len(tokens))
		}
	}
}

func TestAPITokenPrefixStorage(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	userID, _ := db.CreateUser(ctx, "admin", "password123456", "admin", "Admin", false)
	_, token, err := db.CreateAPIToken(ctx, "test-token", userID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Verify prefix was stored
	tokens, _ := db.APITokens(ctx)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}

	expectedPrefix := token
	if len(token) > 12 {
		expectedPrefix = token[:12]
	}

	if tokens[0].Prefix != expectedPrefix {
		t.Errorf("expected prefix %q, got %q", expectedPrefix, tokens[0].Prefix)
	}
}
