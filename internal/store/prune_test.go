package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// count is the smallest thing that can tell this test what happened.
func count(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.R.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func daysAgo(n int) string {
	return time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02T15:04:05Z")
}

// Retention is deletion, so the test that matters is the one that proves what
// survives. Every exemption on the Settings · Retention page is asserted here:
// the current snapshot, the newest N per instance, and both halves of a drift
// comparison that found something.
func TestPruneKeepsWhatTheRetentionPagePromises(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	// Tight windows, and a floor of three rather than the default ten so the
	// fixture stays readable. Three is chosen so the floor covers exactly the
	// three newest snapshots and the drift exemption is what saves the next two —
	// each exemption is then tested by itself.
	for k, v := range map[string]string{
		"snapshot_retention_days":             "30",
		"snapshot_retention_min_per_instance": "3",
		"job_log_retention_days":              "10",
		"probe_retention_days":                "10",
		"audit_retention_days":                "10",
	} {
		if err := db.SetSetting(ctx, k, v, nil); err != nil {
			t.Fatal(err)
		}
	}

	nodeID, err := db.AddNode(ctx, "10.0.0.1", 22, "web01", "nagipath", nil, nil, "manual", nil)
	if err != nil {
		t.Fatal(err)
	}
	instID, err := db.UpsertInstance(ctx, Instance{
		NodeID: nodeID, Vendor: "nginx", NaturalKey: "/etc/nginx/nginx.conf",
		DisplayName: "web01 nginx", MainConfigPath: "/etc/nginx/nginx.conf",
	})
	if err != nil {
		t.Fatal(err)
	}

	// One collection per snapshot, all of them old enough for the job-log window,
	// because a collection is the log line of a run.
	snap := func(ageDays int, current bool) int64 {
		res, err := db.W.ExecContext(ctx, `INSERT INTO collection
			(node_id, trigger, started_at, finished_at, status, instances_seen)
			VALUES (?, 'scheduled', ?, ?, 'succeeded', 1)`, nodeID, daysAgo(ageDays), daysAgo(ageDays))
		if err != nil {
			t.Fatal(err)
		}
		colID, _ := res.LastInsertId()
		cur := 0
		if current {
			cur = 1
		}
		res, err = db.W.ExecContext(ctx, `INSERT INTO snapshot
			(collection_id, instance_id, captured_at, config_source, degraded, content_sha256,
			 file_count, bytes_raw, parse_state, is_current)
			VALUES (?, ?, ?, 'vendor_dump', 0, ?, 1, 100, 'parsed', ?)`,
			colID, instID, daysAgo(ageDays), fmt.Sprintf("sha-%d-%v", ageDays, current), cur)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}

	current := snap(0, true)     // inside the window and current
	recent := snap(5, false)     // inside the window
	newestOld := snap(40, false) // outside the window, but inside the newest-3 floor
	driftSubject := snap(100, false)
	driftBaseline := snap(101, false)
	plainOld := snap(200, false)

	// A drift run with findings pins both of its snapshots.
	if _, err := db.W.ExecContext(ctx, `INSERT INTO drift_run
		(instance_id, subject_snapshot_id, baseline_snapshot_id, baseline_kind,
		 computed_at, parser_version, finding_count)
		VALUES (?, ?, ?, 'previous_snapshot', ?, ?, 3)`,
		instID, driftSubject, driftBaseline, daysAgo(100), ParserVersion); err != nil {
		t.Fatal(err)
	}

	// A probe and an audit event on each side of their windows.
	userID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "admin", false)
	if err != nil {
		t.Fatal(err)
	}
	for i, age := range []int{2, 40} {
		if _, err := db.W.ExecContext(ctx, `INSERT INTO probe
			(actor_user_id, method, url, correlation_token, origin_host, requested_at, result)
			VALUES (?, 'GET', 'https://shop.example.com/', ?, 'nagipath', ?, 'completed')`,
			userID, fmt.Sprintf("tok-%d", i), daysAgo(age)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.W.ExecContext(ctx, `INSERT INTO audit_event
			(at, actor_label, action, outcome) VALUES (?, 'admin', 'auth.login', 'success')`,
			daysAgo(age)); err != nil {
			t.Fatal(err)
		}
	}

	before := count(t, db, "snapshot")
	p, err := db.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Snapshots != 1 {
		t.Errorf("pruned %d snapshots, want 1 (only the 200-day-old one is eligible)", p.Snapshots)
	}
	if got := count(t, db, "snapshot"); got != before-1 {
		t.Errorf("snapshot count %d, want %d", got, before-1)
	}
	alive := func(id int64) bool {
		var n int
		db.R.QueryRow(`SELECT COUNT(*) FROM snapshot WHERE id = ?`, id).Scan(&n)
		return n == 1
	}
	for _, keep := range []struct {
		id  int64
		why string
	}{
		{current, "the current snapshot"},
		{recent, "a snapshot inside the window"},
		{newestOld, "the newest snapshot of the instance"},
		{driftSubject, "the subject of a drift run with findings"},
		{driftBaseline, "the baseline of a drift run with findings"},
	} {
		if !alive(keep.id) {
			t.Errorf("prune deleted %s", keep.why)
		}
	}
	if alive(plainOld) {
		t.Error("prune kept a 200-day-old snapshot that nothing exempts")
	}

	if p.Probes != 1 {
		t.Errorf("pruned %d probes, want 1", p.Probes)
	}
	if p.AuditEvents != 1 {
		t.Errorf("pruned %d audit events, want 1", p.AuditEvents)
	}
	// The 200-day collection lost its snapshot in this same pass, so its log line
	// is now free. The others still hold retained snapshots and must stay.
	if p.JobLogs != 1 {
		t.Errorf("pruned %d job logs, want 1 — a collection is only free once its snapshots are gone", p.JobLogs)
	}

	// Idempotent: a second pass in the same hour has nothing left to do.
	again, err := db.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.Any() {
		t.Errorf("a second pass deleted more: %+v", again)
	}
}

// 0 means keep forever, which is the setting an operator picks when their
// compliance answer is "we never delete the audit trail".
func TestPruneWindowOfZeroKeepsEverything(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "prune0.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SetSetting(ctx, "audit_retention_days", "0", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.W.ExecContext(ctx, `INSERT INTO audit_event
		(at, actor_label, action, outcome) VALUES (?, 'admin', 'auth.login', 'success')`,
		daysAgo(5000)); err != nil {
		t.Fatal(err)
	}
	p, err := db.Prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.AuditEvents != 0 || count(t, db, "audit_event") != 1 {
		t.Errorf("a window of 0 pruned %d audit events", p.AuditEvents)
	}
}
