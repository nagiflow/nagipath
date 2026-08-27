package store

import (
	"context"
	"fmt"
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

// The Changed column of the fleet-wide snapshot list is a digest comparison
// against the instance's previous capture, done with a window function. Off-by-one
// in that comparison is invisible on screen — every row just reads "changed" or
// every row reads "same" — so it gets a test: three captures where the middle one
// differs, and only the middle one may be a change.
func TestSnapshotsSinceMarksOnlyRealChanges(t *testing.T) {
	ctx := context.Background()
	db := snapshotTestDB(t)

	nodeID, err := db.AddNode(ctx, "10.0.0.1", 22, "n1", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, Instance{NodeID: nodeID, Vendor: "nginx",
		NaturalKey: "/etc/nginx/nginx.conf", DisplayName: "n1 nginx",
		MainConfigPath: "/etc/nginx/nginx.conf"})
	if err != nil {
		t.Fatal(err)
	}
	colID, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}

	// captured_at is explicit and ascending: the window orders by it, and two
	// captures sharing a timestamp would make the result order arbitrary.
	for i, body := range []string{"worker_processes 1;", "worker_processes 4;", "worker_processes 4;"} {
		if _, err := db.WriteSnapshot(ctx, Snapshot{
			InstanceID: instID, CollectionID: colID, ConfigSource: "vendor_dump",
			CapturedAt: fmt.Sprintf("2026-01-0%dT00:00:00Z", i+1),
		}, []SnapshotFile{{Path: "/etc/nginx/nginx.conf", Kind: "config_file",
			Content: []byte(body)}}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := db.SnapshotsSince(ctx, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("SnapshotsSince returned %d rows, want 3", len(rows))
	}
	// Newest first, so: same (3 == 2), changed (2 != 1), and the oldest, which has
	// nothing before it and is therefore not a change.
	for i, want := range []bool{false, true, false} {
		if rows[i].Changed != want {
			t.Errorf("row %d (%s) Changed = %v, want %v", i, rows[i].CapturedAt,
				rows[i].Changed, want)
		}
	}
	if rows[0].Instance != "n1 nginx" || rows[0].Trigger != "manual" {
		t.Errorf("joins are wrong: instance %q trigger %q", rows[0].Instance, rows[0].Trigger)
	}

	// The window must not be a lie: a `since` after every capture returns nothing.
	later, err := db.SnapshotsSince(ctx, "2027-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(later) != 0 {
		t.Errorf("SnapshotsSince past every capture returned %d rows", len(later))
	}
}
