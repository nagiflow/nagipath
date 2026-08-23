package store

import (
	"context"
	"path/filepath"
	"testing"
)

func snapshotTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestReconcileInterruptedCollections is the one check behind
// ReconcileInterruptedCollections: a Collection left in status 'running' by a
// process that died mid-collection must be marked failed on the next
// startup, and a Collection that finished normally must be left alone.
func TestReconcileInterruptedCollections(t *testing.T) {
	ctx := context.Background()
	db := snapshotTestDB(t)

	nodeID, err := db.AddNode(ctx, "10.0.0.1", 22, "n1", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}

	stuck, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}

	done, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.FinishCollection(ctx, done, "succeeded", "", 1, 100, 50); err != nil {
		t.Fatal(err)
	}

	n, err := db.ReconcileInterruptedCollections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("ReconcileInterruptedCollections affected %d rows, want 1", n)
	}

	cols, err := db.Collections(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]Collection{}
	for _, c := range cols {
		byID[c.ID] = c
	}
	if got := byID[stuck].Status; got != "failed" {
		t.Errorf("stuck collection status = %q, want failed", got)
	}
	if !byID[stuck].FinishedAt.Valid {
		t.Error("stuck collection should have a finished_at timestamp after reconciliation")
	}
	if got := byID[done].Status; got != "succeeded" {
		t.Errorf("completed collection status = %q, want succeeded (must not be touched)", got)
	}

	// A second run must be a no-op: nothing left in 'running'.
	n, err = db.ReconcileInterruptedCollections(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("second ReconcileInterruptedCollections affected %d rows, want 0", n)
	}
}
