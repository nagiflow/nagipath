package store

import (
	"context"
	"path/filepath"
	"testing"
)

func countsTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "counts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// "Indexed rules" is a fleet total, and a total is wrong in a way nobody can see
// on screen: superseded snapshots keep their rules, so counting rules instead of
// current rules silently grows the number on every collection.
func TestRuleCountIsTheConfigurationInForce(t *testing.T) {
	db := countsTestDB(t)
	ctx := context.Background()

	if n, err := db.RuleCount(ctx); err != nil || n != 0 {
		t.Fatalf("rule count of an empty install = %d, %v; want 0", n, err)
	}

	nodeID, err := db.AddNode(ctx, "10.90.4.2", 22, "web02", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "web02 nginx", MainConfigPath: "/etc/nginx/nginx.conf",
	})
	if err != nil {
		t.Fatal(err)
	}
	colID, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Two captures. The first is superseded by the second, which is what
	// WriteSnapshot's is_current flag records.
	snap := func(body string) int64 {
		id, err := db.WriteSnapshot(ctx, Snapshot{
			InstanceID: instID, CollectionID: colID, ConfigSource: "vendor_dump",
		}, []SnapshotFile{{Path: "/etc/nginx/nginx.conf", Kind: "config_file",
			Content: []byte(body)}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// Rules are inserted directly: what is under test is which snapshot they are
	// counted from, not how the parser produced them.
	addRules := func(snapID int64, n int) {
		files, err := db.SnapshotFiles(ctx, snapID)
		if err != nil || len(files) == 0 {
			t.Fatal(err)
		}
		for i := range n {
			if _, err := db.W.ExecContext(ctx, `INSERT INTO rule
				(snapshot_id, instance_id, parser_version, natural_key, ordinal,
				 prov_file_id, prov_byte_start, prov_byte_end, scope_kind, directive,
				 action_class, args, raw_text, is_modelled)
				VALUES (?,?,1,?,?,?,0,0,'global','worker_processes','other','','worker_processes 4;',1)`,
				snapID, instID, "rule", i, files[0].ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	addRules(snap("worker_processes 1;"), 2)
	addRules(snap("worker_processes 4;"), 3)

	if n, err := db.RuleCount(ctx); err != nil || n != 3 {
		t.Errorf("rule count = %d, %v; want 3 — the superseded capture's rules "+
			"are history, not configuration in force", n, err)
	}

	// A retired instance is not serving anything, so its rules are not the fleet's.
	if err := db.RetireMissingInstances(ctx, nodeID, nil); err != nil {
		t.Fatal(err)
	}
	if n, err := db.RuleCount(ctx); err != nil || n != 0 {
		t.Errorf("rule count after the instance was retired = %d, %v; want 0", n, err)
	}
}
