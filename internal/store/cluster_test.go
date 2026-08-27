package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func clusterTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "cluster.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// clusterFixture is one cluster with `n` instances, each holding one current
// snapshot, which is the shape both cluster queries are asked about.
func clusterFixture(t *testing.T, db *DB, n int) (clusterID int64, instances, snapshots []int64) {
	t.Helper()
	ctx := context.Background()
	res, err := db.W.ExecContext(ctx,
		`INSERT INTO cluster (name, description, config_hash, created_at) VALUES (?,?,?,?)`,
		"web tier", "", "hash123", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	clusterID, _ = res.LastInsertId()

	nodeID, err := db.AddNode(ctx, "10.0.0.1", 22, "n1", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	colID, err := db.StartCollection(ctx, nodeID, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		instID, err := db.UpsertInstance(ctx, Instance{
			NodeID: nodeID, Vendor: "nginx",
			ClusterID:   sql.NullInt64{Int64: clusterID, Valid: true},
			NaturalKey:  fmt.Sprintf("/etc/nginx/%d.conf", i),
			DisplayName: fmt.Sprintf("web%02d nginx", i),
		})
		if err != nil {
			t.Fatal(err)
		}
		snapID, err := db.WriteSnapshot(ctx, Snapshot{
			InstanceID: instID, CollectionID: colID, ConfigSource: "vendor_dump",
			CapturedAt: fmt.Sprintf("2026-01-0%dT00:00:00Z", i+1),
		}, []SnapshotFile{{Path: "/etc/nginx/nginx.conf", Kind: "config_file",
			Content: []byte(fmt.Sprintf("worker_processes %d;", i))}})
		if err != nil {
			t.Fatal(err)
		}
		instances, snapshots = append(instances, instID), append(snapshots, snapID)
	}
	return clusterID, instances, snapshots
}

// driftRun records a comparison with `findings` differences, `ignored` of which an
// operator has chosen to ignore.
func driftRun(t *testing.T, db *DB, clusterID, instID, snapID int64, findings, ignored int) {
	t.Helper()
	ctx := context.Background()
	res, err := db.W.ExecContext(ctx, `INSERT INTO drift_run
		(instance_id, subject_snapshot_id, baseline_kind, computed_at, parser_version)
		VALUES (?,?,'previous_snapshot','2026-02-01T00:00:00Z',?)`,
		instID, snapID, ParserVersion)
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := res.LastInsertId()
	// An ignore rule has to exist for a finding to point at one, because the
	// column is a foreign key: "ignored" is a decision with an author.
	var ruleID int64
	if ignored > 0 {
		r, err := db.W.ExecContext(ctx, `INSERT INTO drift_ignore_rule
			(cluster_id, object_kind, pattern, reason, created_at)
			VALUES (?,'rule','worker_processes','lab noise','2026-01-01T00:00:00Z')`, clusterID)
		if err != nil {
			t.Fatal(err)
		}
		ruleID, _ = r.LastInsertId()
	}
	for i := range findings {
		var rule any
		if i < ignored {
			rule = ruleID
		}
		if _, err := db.W.ExecContext(ctx, `INSERT INTO drift_finding
			(drift_run_id, object_kind, natural_key, change, ignored_by_rule_id)
			VALUES (?,'rule',?,'changed',?)`,
			runID, fmt.Sprintf("rule/%d", i), rule); err != nil {
			t.Fatal(err)
		}
	}
}

// The cluster list's three columns are all counts of things that live one or two
// joins away, and a count is wrong in a way nobody notices on screen.
func TestClusterAggregatesCountsWhatTheColumnsClaim(t *testing.T) {
	ctx := context.Background()
	db := clusterTestDB(t)
	clusterID, instances, snapshots := clusterFixture(t, db, 3)

	// The first instance diverges (two findings, one of them ignored, so it counts
	// once), the second was compared and matched, the third was never compared.
	driftRun(t, db, clusterID, instances[0], snapshots[0], 2, 1)
	driftRun(t, db, clusterID, instances[1], snapshots[1], 0, 0)

	// One certificate that expired last week and one with a year left. Expired is
	// inside the window, not outside it: a column that dropped it would hide the
	// worse case behind the milder one.
	for i, notAfter := range []string{
		time.Now().Add(-7 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z"),
		time.Now().Add(400 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z"),
	} {
		certID, err := db.UpsertCertificate(ctx, Certificate{
			Fingerprint: fmt.Sprintf("fp%d", i), SubjectCN: "shop.example.com",
			NotBefore: "2026-01-01T00:00:00Z", NotAfter: notAfter,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AddCertBinding(ctx, CertBindingRow{
			CertificateID: certID, InstanceID: instances[0], SnapshotID: snapshots[0],
			FilePath: "/etc/nginx/certs/shop.crt",
		}); err != nil {
			t.Fatal(err)
		}
	}

	aggs, err := db.ClusterAggregates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	agg, ok := aggs[clusterID]
	if !ok {
		t.Fatalf("cluster %d is missing from the aggregates", clusterID)
	}
	if agg.DriftCount != 1 {
		t.Errorf("drift = %d, want 1: one member diverges, one matched, one was "+
			"never compared, and an ignored finding is not divergence", agg.DriftCount)
	}
	if agg.CertsExpiring30d != 1 {
		t.Errorf("certs inside the horizon = %d, want 1 (the expired one counts, "+
			"the one with a year left does not)", agg.CertsExpiring30d)
	}
	if agg.LastCollected.String != "2026-01-03T00:00:00Z" {
		t.Errorf("last collected = %q, want the newest capture in the cluster",
			agg.LastCollected.String)
	}
}

// "clean" and "never compared" are different answers, and the members panel says
// them differently. A COUNT(*) cannot tell them apart, so the query must not use
// one at the top level.
func TestClusterMembersSeparatesCleanFromNeverCompared(t *testing.T) {
	db := clusterTestDB(t)
	clusterID, instances, snapshots := clusterFixture(t, db, 3)
	driftRun(t, db, clusterID, instances[0], snapshots[0], 3, 1)
	driftRun(t, db, clusterID, instances[1], snapshots[1], 0, 0)

	members, err := db.ClusterMembers(context.Background(), clusterID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 3 {
		t.Fatalf("members = %d, want 3", len(members))
	}
	// Ordered by display name, which the fixture numbers in creation order.
	for i, want := range []struct {
		valid bool
		n     int64
	}{{true, 2}, {true, 0}, {false, 0}} {
		got := members[i].Divergence
		if got.Valid != want.valid || got.Int64 != want.n {
			t.Errorf("%s divergence = %v (valid %v), want %d (valid %v)",
				members[i].DisplayName, got.Int64, got.Valid, want.n, want.valid)
		}
	}
	if members[0].NodeDisplayName != "n1" {
		t.Errorf("node join is empty: %q", members[0].NodeDisplayName)
	}
}
