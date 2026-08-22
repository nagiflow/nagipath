package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Drift compares parsed objects, never file text: two Instances that say the same
// thing in different words are not different, and one that says the same words in
// a different order is. Both sides are always at the current ParserVersion —
// ComputeDrift refuses otherwise — which makes phantom drift from a parser change
// structurally impossible rather than merely unlikely (drift.md §4.1).
//
// The vocabulary is deliberate. A finding is a divergence, not a violation;
// nagipath has no opinion about which side is correct.

// ------------------------------------------------------------------- clusters

// A Cluster is an operator's declaration that these Instances are meant to be
// alike. nagipath never infers one: "meant to be alike" is a statement about
// intent, and a wrong guess turns every legitimate difference into noise.
type Cluster struct {
	ID             int64
	Name           string
	Description    string
	GoldenPeer     sql.NullInt64
	GoldenPeerName string
	CreatedAt      string
	Members        int
}

func (db *DB) Clusters(ctx context.Context) ([]Cluster, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT c.id, c.name, c.description,
		c.golden_peer_instance_id, COALESCE(g.display_name, ''), c.created_at,
		(SELECT COUNT(*) FROM instance i WHERE i.cluster_id = c.id AND i.retired_at IS NULL)
		FROM cluster c
		LEFT JOIN instance g ON g.id = c.golden_peer_instance_id
		ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.GoldenPeer,
			&c.GoldenPeerName, &c.CreatedAt, &c.Members); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetGoldenPeer records which Instance an operator considers correct. It is the
// only place in nagipath where one configuration outranks another, and it is
// always a human saying so.
func (db *DB) SetGoldenPeer(ctx context.Context, clusterID int64, instanceID *int64, by *int64) error {
	if instanceID != nil {
		var in sql.NullInt64
		if err := db.R.QueryRowContext(ctx,
			`SELECT cluster_id FROM instance WHERE id = ?`, *instanceID).Scan(&in); err != nil {
			return err
		}
		if !in.Valid || in.Int64 != clusterID {
			return fmt.Errorf("an instance can only be the golden peer of its own cluster")
		}
	}
	if _, err := db.W.ExecContext(ctx,
		`UPDATE cluster SET golden_peer_instance_id = ? WHERE id = ?`, instanceID, clusterID); err != nil {
		return err
	}
	db.Audit(ctx, by, "cluster.golden_peer", "cluster", &clusterID, "")
	return nil
}

// ---------------------------------------------------------------- ignore rules

// An IgnoreRule marks findings rather than deleting them: the ignored count stays
// beside the finding count on every screen, because a rule that quietly hides a
// growing pile is how a real divergence gets missed. A reason is mandatory —
// six months from now nobody remembers why.
type IgnoreRule struct {
	ID         int64
	ClusterID  int64
	ObjectKind string
	Field      string
	Pattern    string
	Reason     string
	CreatedAt  string
	Matched    int
}

func (db *DB) IgnoreRules(ctx context.Context, clusterID int64) ([]IgnoreRule, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT r.id, r.cluster_id, r.object_kind, r.field,
		r.pattern, r.reason, r.created_at,
		(SELECT COUNT(*) FROM drift_finding f WHERE f.ignored_by_rule_id = r.id)
		FROM drift_ignore_rule r WHERE r.cluster_id = ? ORDER BY r.id`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IgnoreRule
	for rows.Next() {
		var r IgnoreRule
		if err := rows.Scan(&r.ID, &r.ClusterID, &r.ObjectKind, &r.Field, &r.Pattern,
			&r.Reason, &r.CreatedAt, &r.Matched); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) AddIgnoreRule(ctx context.Context, r IgnoreRule, by *int64) (int64, error) {
	r.ObjectKind = strings.TrimSpace(r.ObjectKind)
	r.Pattern = strings.TrimSpace(r.Pattern)
	r.Reason = strings.TrimSpace(r.Reason)
	if r.ObjectKind == "" || r.Pattern == "" {
		return 0, fmt.Errorf("an ignore rule needs an object kind and a pattern")
	}
	if r.Reason == "" {
		return 0, fmt.Errorf("an ignore rule needs a reason: an unexplained one is indistinguishable from a bug")
	}
	res, err := db.W.ExecContext(ctx, `INSERT INTO drift_ignore_rule
		(cluster_id, object_kind, field, pattern, reason, created_at, created_by)
		VALUES (?,?,?,?,?,?,?)`,
		r.ClusterID, r.ObjectKind, r.Field, r.Pattern, r.Reason, Now(), by)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	db.Audit(ctx, by, "drift.ignore_rule.create", "cluster", &r.ClusterID, r.Pattern)
	return id, nil
}

func (db *DB) DeleteIgnoreRule(ctx context.Context, id int64, by *int64) error {
	if _, err := db.W.ExecContext(ctx,
		`DELETE FROM drift_ignore_rule WHERE id = ?`, id); err != nil {
		return err
	}
	db.Audit(ctx, by, "drift.ignore_rule.delete", "drift_ignore_rule", &id, "")
	return nil
}

// ------------------------------------------------------------------ runs

type DriftRun struct {
	ID                 int64
	InstanceID         int64
	InstanceName       string
	NodeName           string
	Vendor             string
	SubjectSnapshotID  int64
	BaselineKind       string
	BaselineSnapshotID sql.NullInt64
	BaselineLabel      string
	ComputedAt         string
	ParserVersion      int
	FindingCount       int
	IgnoredCount       int
	IsGoldenPeer       bool
}

type DriftFinding struct {
	ID           int64
	RunID        int64
	InstanceID   int64
	InstanceName string
	ObjectKind   string
	NaturalKey   string
	Change       string
	Field        string
	BaselineText string
	SubjectText  string
	ActionClass  string
	FileID       sql.NullInt64
	SnapshotID   sql.NullInt64
	ByteStart    sql.NullInt64
	ByteEnd      sql.NullInt64
	IgnoredBy    sql.NullInt64
	IgnoreReason string
}

// BaselineLabels are the words for the three baselines, in priority order.
var BaselineLabels = []struct{ Kind, Label string }{
	{"golden_peer", "golden peer"},
	{"cluster_majority", "cluster majority"},
	{"previous_snapshot", "previous snapshot"},
}

func baselineLabel(kind string) string {
	for _, b := range BaselineLabels {
		if b.Kind == kind {
			return b.Label
		}
	}
	return kind
}

// DriftRuns returns the newest run per (instance, baseline kind) for one Cluster,
// or for the whole fleet when clusterID is 0.
func (db *DB) DriftRuns(ctx context.Context, clusterID int64, kind string) ([]DriftRun, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT r.id, r.instance_id, i.display_name,
		n.display_name, i.vendor, r.subject_snapshot_id, r.baseline_kind,
		r.baseline_snapshot_id, r.computed_at, r.parser_version,
		r.finding_count, r.ignored_count,
		COALESCE(c.golden_peer_instance_id = i.id, 0)
		FROM drift_run r
		JOIN instance i ON i.id = r.instance_id
		JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster c ON c.id = i.cluster_id
		WHERE (? = 0 OR i.cluster_id = ?) AND (? = '' OR r.baseline_kind = ?)
		  AND r.computed_at = (SELECT MAX(computed_at) FROM drift_run x
		      WHERE x.instance_id = r.instance_id AND x.baseline_kind = r.baseline_kind)
		ORDER BY i.display_name, r.baseline_kind`,
		clusterID, clusterID, kind, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DriftRun
	for rows.Next() {
		var r DriftRun
		if err := rows.Scan(&r.ID, &r.InstanceID, &r.InstanceName, &r.NodeName, &r.Vendor,
			&r.SubjectSnapshotID, &r.BaselineKind, &r.BaselineSnapshotID, &r.ComputedAt,
			&r.ParserVersion, &r.FindingCount, &r.IgnoredCount, &r.IsGoldenPeer); err != nil {
			return nil, err
		}
		r.BaselineLabel = baselineLabel(r.BaselineKind)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DriftFindings reads the findings of the given runs, ignored ones included and
// labelled. Hiding them would make an over-broad ignore rule invisible.
func (db *DB) DriftFindings(ctx context.Context, runIDs []int64) ([]DriftFinding, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}
	holes := strings.TrimSuffix(strings.Repeat("?,", len(runIDs)), ",")
	args := make([]any, len(runIDs))
	for i, id := range runIDs {
		args[i] = id
	}
	rows, err := db.R.QueryContext(ctx, `SELECT f.id, f.drift_run_id, r.instance_id,
		i.display_name, f.object_kind, f.natural_key, f.change, f.field,
		f.baseline_text, f.subject_text, f.action_class,
		f.prov_file_id, sf.snapshot_id, f.prov_byte_start, f.prov_byte_end,
		f.ignored_by_rule_id, COALESCE(ir.reason, '')
		FROM drift_finding f
		JOIN drift_run r ON r.id = f.drift_run_id
		JOIN instance i ON i.id = r.instance_id
		LEFT JOIN snapshot_file sf ON sf.id = f.prov_file_id
		LEFT JOIN drift_ignore_rule ir ON ir.id = f.ignored_by_rule_id
		WHERE f.drift_run_id IN (`+holes+`)
		ORDER BY f.object_kind, f.natural_key, i.display_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DriftFinding
	for rows.Next() {
		var f DriftFinding
		if err := rows.Scan(&f.ID, &f.RunID, &f.InstanceID, &f.InstanceName, &f.ObjectKind,
			&f.NaturalKey, &f.Change, &f.Field, &f.BaselineText, &f.SubjectText,
			&f.ActionClass, &f.FileID, &f.SnapshotID, &f.ByteStart, &f.ByteEnd,
			&f.IgnoredBy, &f.IgnoreReason); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------ comparison

// driftObject is one derived row reduced to what a comparison needs.
type driftObject struct {
	Kind        string
	Key         string
	Ordinal     int
	Text        string
	ActionClass string
	FileID      sql.NullInt64
	Start       sql.NullInt64
	End         sql.NullInt64
}

// driftObjects reads a Snapshot's parsed objects. site_name is intentionally
// absent: a name change shows up as a changed site, and reporting both would
// double-count one edit.
func (db *DB) driftObjects(ctx context.Context, snapshotID int64) ([]driftObject, error) {
	const q = `
	  SELECT 'listener', natural_key, ordinal, raw_text, '',
	         prov_file_id, prov_byte_start, prov_byte_end FROM listener WHERE snapshot_id = ?
	  UNION ALL SELECT 'site', natural_key, ordinal, raw_text, '',
	         prov_file_id, prov_byte_start, prov_byte_end FROM site WHERE snapshot_id = ?
	  UNION ALL SELECT 'route', natural_key, ordinal, raw_text, '',
	         prov_file_id, prov_byte_start, prov_byte_end FROM route WHERE snapshot_id = ?
	  UNION ALL SELECT 'upstream', natural_key, ordinal, raw_text, '',
	         prov_file_id, prov_byte_start, prov_byte_end FROM upstream WHERE snapshot_id = ?
	  UNION ALL SELECT 'upstream_member', natural_key, ordinal, raw_text, '',
	         prov_file_id, prov_byte_start, prov_byte_end FROM upstream_member WHERE snapshot_id = ?
	  UNION ALL SELECT 'rule', natural_key, ordinal, raw_text, action_class,
	         prov_file_id, prov_byte_start, prov_byte_end FROM rule WHERE snapshot_id = ?`
	rows, err := db.R.QueryContext(ctx, q, snapshotID, snapshotID, snapshotID,
		snapshotID, snapshotID, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []driftObject
	for rows.Next() {
		var o driftObject
		if err := rows.Scan(&o.Kind, &o.Key, &o.Ordinal, &o.Text, &o.ActionClass,
			&o.FileID, &o.Start, &o.End); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// indexObjects keys objects for comparison. A natural key can legitimately repeat
// (two `location /api` blocks in one file), so repeats are disambiguated by their
// order of appearance rather than merged.
func indexObjects(list []driftObject) map[string]driftObject {
	out := make(map[string]driftObject, len(list))
	seen := map[string]int{}
	for _, o := range list {
		base := o.Kind + "\x00" + o.Key
		n := seen[base]
		seen[base] = n + 1
		if n > 0 {
			base = fmt.Sprintf("%s#%d", base, n)
		}
		out[base] = o
	}
	return out
}

// diffObjects is the whole comparison. `reordered` is a real change and is
// reported as one: in every vendor here, order decides which rule wins.
func diffObjects(baseline, subject []driftObject) []DriftFinding {
	bi, si := indexObjects(baseline), indexObjects(subject)
	keys := make([]string, 0, len(bi)+len(si))
	for k := range si {
		keys = append(keys, k)
	}
	for k := range bi {
		if _, ok := si[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var out []DriftFinding
	for _, k := range keys {
		s, inSubject := si[k]
		b, inBaseline := bi[k]
		switch {
		case !inBaseline:
			out = append(out, finding(s, "added", "", s.Text))
		case !inSubject:
			out = append(out, finding(b, "removed", b.Text, ""))
		case b.Text != s.Text:
			out = append(out, finding(s, "changed", b.Text, s.Text))
		case b.Ordinal != s.Ordinal:
			f := finding(s, "reordered", b.Text, s.Text)
			f.Field = "ordinal"
			f.BaselineText = fmt.Sprintf("position %d", b.Ordinal)
			f.SubjectText = fmt.Sprintf("position %d", s.Ordinal)
			out = append(out, f)
		}
	}
	return out
}

func finding(o driftObject, change, baseline, subject string) DriftFinding {
	return DriftFinding{
		ObjectKind: o.Kind, NaturalKey: o.Key, Change: change,
		BaselineText: baseline, SubjectText: subject, ActionClass: o.ActionClass,
		FileID: o.FileID, ByteStart: o.Start, ByteEnd: o.End,
	}
}

// ------------------------------------------------------------------ baselines

// Baselines lists every comparison that is meaningful for an Instance right now,
// in priority order. All of them are computed, not just the first: an operator who
// declared a golden peer still wants to know what changed since yesterday
// (drift.md §3).
func (db *DB) Baselines(ctx context.Context, instanceID int64) []string {
	var cluster, golden sql.NullInt64
	db.R.QueryRowContext(ctx, `SELECT i.cluster_id, c.golden_peer_instance_id
		FROM instance i LEFT JOIN cluster c ON c.id = i.cluster_id
		WHERE i.id = ?`, instanceID).Scan(&cluster, &golden)

	members := 0
	if cluster.Valid {
		db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance i
			JOIN snapshot s ON s.instance_id = i.id AND s.is_current = 1
			WHERE i.cluster_id = ? AND i.retired_at IS NULL`, cluster.Int64).Scan(&members)
	}
	snapshots := 0
	db.R.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM snapshot WHERE instance_id = ?`, instanceID).Scan(&snapshots)

	var out []string
	if golden.Valid && golden.Int64 != instanceID {
		out = append(out, "golden_peer")
	}
	// A majority needs three opinions; with two there is no majority, only a
	// disagreement, and calling one side the baseline would be a coin toss.
	if members >= 3 {
		out = append(out, "cluster_majority")
	}
	if snapshots >= 2 {
		out = append(out, "previous_snapshot")
	}
	return out
}

// ClusterOfOne reports whether an Instance's cluster has no peers, so the UI can
// say why the only available comparison is against its own history.
func (db *DB) ClusterOfOne(ctx context.Context, instanceID int64) bool {
	var cluster sql.NullInt64
	db.R.QueryRowContext(ctx,
		`SELECT cluster_id FROM instance WHERE id = ?`, instanceID).Scan(&cluster)
	if !cluster.Valid {
		return false
	}
	n := 0
	db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance
		WHERE cluster_id = ? AND retired_at IS NULL`, cluster.Int64).Scan(&n)
	return n == 1
}

// ------------------------------------------------------------------ computation

// ComputeDrift compares an Instance's current Snapshot against one baseline and
// stores the result. It refuses when either side was parsed at a different
// ParserVersion: a comparison across parser versions reports the parser's changes
// as the fleet's, which is the one drift result an operator must never see
// (drift.md §4.1).
func (db *DB) ComputeDrift(ctx context.Context, instanceID int64, kind string) (int64, error) {
	subject, err := db.CurrentSnapshot(ctx, instanceID)
	if err != nil {
		return 0, fmt.Errorf("instance %d has no current snapshot to compare", instanceID)
	}
	if snapshotVersion(subject) != ParserVersion {
		return 0, fmt.Errorf("snapshot %d was parsed at version %d and the current parser is v%d; re-collect before comparing",
			subject.ID, snapshotVersion(subject), ParserVersion)
	}
	subjectObjs, err := db.driftObjects(ctx, subject.ID)
	if err != nil {
		return 0, err
	}

	var baselineSnapshot sql.NullInt64
	var baselineObjs []driftObject
	switch kind {
	case "previous_snapshot":
		var prev Snapshot
		prev, err = db.previousSnapshot(ctx, instanceID, subject.ID)
		if err != nil {
			return 0, err
		}
		if snapshotVersion(prev) != ParserVersion {
			return 0, fmt.Errorf("the previous snapshot was parsed at v%d and the current parser is v%d; re-parse it before comparing",
				snapshotVersion(prev), ParserVersion)
		}
		baselineSnapshot = sql.NullInt64{Int64: prev.ID, Valid: true}
		baselineObjs, err = db.driftObjects(ctx, prev.ID)
	case "golden_peer":
		var peer int64
		if err = db.R.QueryRowContext(ctx, `SELECT c.golden_peer_instance_id
			FROM instance i JOIN cluster c ON c.id = i.cluster_id
			WHERE i.id = ? AND c.golden_peer_instance_id IS NOT NULL`, instanceID).Scan(&peer); err != nil {
			return 0, fmt.Errorf("no golden peer is declared for this instance's cluster")
		}
		if peer == instanceID {
			return 0, fmt.Errorf("this instance is the golden peer; there is nothing to compare it to")
		}
		var snap Snapshot
		snap, err = db.CurrentSnapshot(ctx, peer)
		if err != nil {
			return 0, fmt.Errorf("the golden peer has no current snapshot")
		}
		if snapshotVersion(snap) != ParserVersion {
			return 0, fmt.Errorf("the golden peer's snapshot was parsed at v%d and the current parser is v%d; re-collect it before comparing",
				snapshotVersion(snap), ParserVersion)
		}
		baselineSnapshot = sql.NullInt64{Int64: snap.ID, Valid: true}
		baselineObjs, err = db.driftObjects(ctx, snap.ID)
	case "cluster_majority":
		// No single Snapshot is the baseline here, so baseline_snapshot_id stays
		// NULL and the objects are synthesised from the members.
		baselineObjs, err = db.majorityObjects(ctx, instanceID)
	default:
		return 0, fmt.Errorf("unknown baseline %q", kind)
	}
	if err != nil {
		return 0, err
	}

	findings := diffObjects(baselineObjs, subjectObjs)
	ignored, err := db.applyIgnoreRules(ctx, instanceID, findings)
	if err != nil {
		return 0, err
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// A recomputed run replaces the previous one for the same (instance, subject,
	// baseline): two answers about the same comparison is one answer too many.
	if _, err := tx.ExecContext(ctx, `DELETE FROM drift_run
		WHERE instance_id = ? AND subject_snapshot_id = ? AND baseline_kind = ?`,
		instanceID, subject.ID, kind); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO drift_run
		(instance_id, subject_snapshot_id, baseline_kind, baseline_snapshot_id,
		 computed_at, parser_version, finding_count, ignored_count)
		VALUES (?,?,?,?,?,?,?,?)`,
		instanceID, subject.ID, kind, baselineSnapshot, Now(), ParserVersion,
		len(findings)-ignored, ignored)
	if err != nil {
		return 0, err
	}
	runID, _ := res.LastInsertId()
	for _, f := range findings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO drift_finding
			(drift_run_id, object_kind, natural_key, change, field, baseline_text,
			 subject_text, action_class, prov_file_id, prov_byte_start, prov_byte_end,
			 ignored_by_rule_id)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			runID, f.ObjectKind, f.NaturalKey, f.Change, f.Field, f.BaselineText,
			f.SubjectText, f.ActionClass, f.FileID, f.ByteStart, f.ByteEnd,
			f.IgnoredBy); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return runID, nil
}

func (db *DB) previousSnapshot(ctx context.Context, instanceID, currentID int64) (Snapshot, error) {
	var id int64
	if err := db.R.QueryRowContext(ctx, `SELECT id FROM snapshot
		WHERE instance_id = ? AND id != ? ORDER BY captured_at DESC, id DESC LIMIT 1`,
		instanceID, currentID).Scan(&id); err != nil {
		return Snapshot{}, fmt.Errorf("this instance has only ever been collected once, so there is no earlier snapshot to compare against")
	}
	return db.SnapshotByID(ctx, id)
}

// majorityObjects synthesises the baseline the cluster agrees on. Where no value
// is held by more than half the members there is no majority, and that is
// reported as its own finding rather than resolved by picking one.
func (db *DB) majorityObjects(ctx context.Context, instanceID int64) ([]driftObject, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT s.id, s.parser_version
		FROM instance i
		JOIN snapshot s ON s.instance_id = i.id AND s.is_current = 1
		WHERE i.retired_at IS NULL AND i.cluster_id =
			(SELECT cluster_id FROM instance WHERE id = ?)`, instanceID)
	if err != nil {
		return nil, err
	}
	var snapshots []int64
	for rows.Next() {
		var id, pv int64
		if err := rows.Scan(&id, &pv); err != nil {
			rows.Close()
			return nil, err
		}
		if int(pv) != ParserVersion {
			rows.Close()
			return nil, fmt.Errorf("snapshot %d in this cluster was parsed at v%d and the current parser is v%d; re-collect the cluster before comparing",
				id, pv, ParserVersion)
		}
		snapshots = append(snapshots, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(snapshots) < 3 {
		return nil, fmt.Errorf("a majority needs at least three members; this cluster has %d", len(snapshots))
	}

	// votes[key][text] = how many members say exactly that.
	votes := map[string]map[string]int{}
	proto := map[string]driftObject{}
	for _, snap := range snapshots {
		objs, err := db.driftObjects(ctx, snap)
		if err != nil {
			return nil, err
		}
		for k, o := range indexObjects(objs) {
			if votes[k] == nil {
				votes[k] = map[string]int{}
				proto[k] = o
			}
			votes[k][o.Text]++
		}
	}

	half := len(snapshots) / 2
	var out []driftObject
	for k, tally := range votes {
		best, bestN := "", 0
		for text, n := range tally {
			if n > bestN {
				best, bestN = text, n
			}
		}
		o := proto[k]
		if bestN > half {
			o.Text = best
		} else {
			// No majority: say so in the baseline text so the finding reads as
			// "the cluster does not agree", not "you differ from the cluster".
			variants := make([]string, 0, len(tally))
			for text, n := range tally {
				variants = append(variants, fmt.Sprintf("%d× %s", n, firstLine(text)))
			}
			sort.Strings(variants)
			o.Text = "no_majority: " + strings.Join(variants, " | ")
		}
		out = append(out, o)
	}
	return out, nil
}

// snapshotVersion reports the parser version a Snapshot's derived rows were
// written at. An unparsed Snapshot reports 0, which never equals ParserVersion, so
// it is refused as a side of a comparison rather than compared as if it were empty.
func snapshotVersion(s Snapshot) int {
	if s.ParserVersion.Valid {
		return int(s.ParserVersion.Int64)
	}
	return 0
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// applyIgnoreRules stamps findings that a cluster-scoped rule covers and returns
// how many were stamped. Nothing is dropped.
func (db *DB) applyIgnoreRules(ctx context.Context, instanceID int64, findings []DriftFinding) (int, error) {
	var cluster sql.NullInt64
	db.R.QueryRowContext(ctx,
		`SELECT cluster_id FROM instance WHERE id = ?`, instanceID).Scan(&cluster)
	if !cluster.Valid {
		return 0, nil
	}
	rules, err := db.IgnoreRules(ctx, cluster.Int64)
	if err != nil || len(rules) == 0 {
		return 0, err
	}

	// Per-host values a pattern can stand in for, so one rule covers the cluster
	// instead of one rule per host.
	var host, addr string
	db.R.QueryRowContext(ctx, `SELECT n.display_name, n.address
		FROM instance i JOIN node n ON n.id = i.node_id WHERE i.id = ?`,
		instanceID).Scan(&host, &addr)
	subst := strings.NewReplacer(
		"{{node_hostname}}", host,
		"{{node_display_name}}", host,
		"{{node_primary_ip}}", addr)

	n := 0
	for i := range findings {
		for _, r := range rules {
			if r.ObjectKind != findings[i].ObjectKind {
				continue
			}
			if r.Field != "" && r.Field != findings[i].Field {
				continue
			}
			// ponytail: case-insensitive substring, not a glob. Upgrade to
			// path.Match if operators start writing `*` and expecting it to anchor.
			pat := strings.ToLower(subst.Replace(r.Pattern))
			hay := strings.ToLower(findings[i].NaturalKey + "\n" +
				findings[i].BaselineText + "\n" + findings[i].SubjectText)
			if strings.Contains(hay, pat) {
				findings[i].IgnoredBy = sql.NullInt64{Int64: r.ID, Valid: true}
				n++
				break
			}
		}
	}
	return n, nil
}

// RecomputeCluster recomputes every applicable baseline for every member of a
// Cluster, or for every Instance in the fleet when clusterID is 0. It returns the
// number of runs stored and the reasons any were skipped — a skipped comparison
// is reported, never silently omitted.
func (db *DB) RecomputeCluster(ctx context.Context, clusterID int64) (int, []string) {
	rows, err := db.R.QueryContext(ctx, `SELECT i.id, i.display_name FROM instance i
		JOIN snapshot s ON s.instance_id = i.id AND s.is_current = 1
		WHERE i.retired_at IS NULL AND (? = 0 OR i.cluster_id = ?)
		ORDER BY i.display_name`, clusterID, clusterID)
	if err != nil {
		return 0, []string{err.Error()}
	}
	type target struct {
		id   int64
		name string
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.name); err == nil {
			targets = append(targets, t)
		}
	}
	rows.Close()

	stored := 0
	var skipped []string
	for _, t := range targets {
		kinds := db.Baselines(ctx, t.id)
		if len(kinds) == 0 {
			skipped = append(skipped, t.name+": no comparison is possible yet (collect it twice, or put it in a cluster)")
			continue
		}
		for _, k := range kinds {
			if _, err := db.ComputeDrift(ctx, t.id, k); err != nil {
				skipped = append(skipped, fmt.Sprintf("%s vs %s: %v", t.name, baselineLabel(k), err))
				continue
			}
			stored++
		}
	}
	return stored, skipped
}
