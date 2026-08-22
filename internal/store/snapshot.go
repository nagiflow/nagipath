package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

type Instance struct {
	ID             int64
	NodeID         int64
	ClusterID      sql.NullInt64
	Vendor         string
	NaturalKey     string
	DisplayName    string
	Version        string
	BinaryPath     string
	ConfigRoot     string
	MainConfigPath string
	BuildFlags     string
	ServiceManager string
	UnitName       string
	DetectedPID    sql.NullInt64
	AccessLogPaths []string
	FirstSeenAt    string
	LastSeenAt     string
	RetiredAt      sql.NullString

	// Joined for list views.
	NodeAddress     string
	NodeDisplayName string
	SiteCount       int
	LastCaptured    sql.NullString
	ClusterName     string
	Degraded        bool
	ParseState      string
	// Ports is the comma-separated set of ports the current Snapshot listens on, so
	// the fleet list can say ":443 · 2 sites" without a second query per row.
	Ports string
}

// State is the one word the fleet list shows. `pending` means nothing has been
// collected yet, which is a different thing from healthy and must not read as one.
func (in Instance) State() string {
	switch {
	case !in.LastCaptured.Valid:
		return "pending"
	case in.ParseState != "" && in.ParseState != "parsed":
		return "unparsed"
	case in.Degraded:
		return "degraded"
	}
	return "ok"
}

// UpsertInstance is keyed on (node, vendor, natural_key) so a restart with a new
// PID is the same Instance, not a new one.
func (db *DB) UpsertInstance(ctx context.Context, in Instance) (int64, error) {
	logs, err := json.Marshal(in.AccessLogPaths)
	if err != nil {
		return 0, err
	}
	if in.AccessLogPaths == nil {
		logs = []byte("[]")
	}
	if in.ServiceManager == "" {
		in.ServiceManager = "unknown"
	}
	now := Now()
	_, err = db.W.ExecContext(ctx, `INSERT INTO instance
		(node_id, cluster_id, vendor, natural_key, display_name, version, binary_path,
		 config_root, main_config_path, build_flags, service_manager, unit_name,
		 detected_pid, access_log_paths, first_seen_at, last_seen_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(node_id, vendor, natural_key) DO UPDATE SET
		  display_name = excluded.display_name,
		  version = excluded.version,
		  binary_path = excluded.binary_path,
		  config_root = excluded.config_root,
		  main_config_path = excluded.main_config_path,
		  build_flags = excluded.build_flags,
		  service_manager = excluded.service_manager,
		  unit_name = excluded.unit_name,
		  detected_pid = excluded.detected_pid,
		  access_log_paths = excluded.access_log_paths,
		  last_seen_at = excluded.last_seen_at,
		  retired_at = NULL`,
		in.NodeID, in.ClusterID, in.Vendor, in.NaturalKey, in.DisplayName, in.Version,
		in.BinaryPath, in.ConfigRoot, in.MainConfigPath, in.BuildFlags, in.ServiceManager,
		in.UnitName, in.DetectedPID, string(logs), now, now)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.R.QueryRowContext(ctx,
		`SELECT id FROM instance WHERE node_id = ? AND vendor = ? AND natural_key = ?`,
		in.NodeID, in.Vendor, in.NaturalKey).Scan(&id)
	return id, err
}

// RetireMissingInstances marks Instances not seen in this collection as retired
// rather than deleting them: their history is still the answer to "what was
// serving this last week?".
func (db *DB) RetireMissingInstances(ctx context.Context, nodeID int64, seen []int64) error {
	query := `UPDATE instance SET retired_at = ? WHERE node_id = ? AND retired_at IS NULL`
	args := []any{Now(), nodeID}
	for _, id := range seen {
		query += ` AND id != ?`
		args = append(args, id)
	}
	_, err := db.W.ExecContext(ctx, query, args...)
	return err
}

func (db *DB) Instances(ctx context.Context) ([]Instance, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT i.id, i.node_id, i.cluster_id, i.vendor,
		i.natural_key, i.display_name, i.version, i.binary_path, i.config_root,
		i.main_config_path, i.service_manager, i.unit_name, i.detected_pid,
		i.access_log_paths, i.first_seen_at, i.last_seen_at, i.retired_at,
		n.address, n.display_name,
		(SELECT COUNT(*) FROM site s JOIN snapshot sn ON sn.id = s.snapshot_id
		   WHERE s.instance_id = i.id AND sn.is_current = 1),
		(SELECT MAX(captured_at) FROM snapshot sn WHERE sn.instance_id = i.id),
		COALESCE(c.name, ''),
		COALESCE((SELECT sn.degraded FROM snapshot sn
		   WHERE sn.instance_id = i.id AND sn.is_current = 1), 0),
		COALESCE((SELECT sn.parse_state FROM snapshot sn
		   WHERE sn.instance_id = i.id AND sn.is_current = 1), ''),
		COALESCE((SELECT group_concat(DISTINCT l.port) FROM listener l
		   JOIN snapshot sn ON sn.id = l.snapshot_id
		   WHERE l.instance_id = i.id AND sn.is_current = 1), '')
		FROM instance i JOIN node n ON n.id = i.node_id
		LEFT JOIN cluster c ON c.id = i.cluster_id
		WHERE i.retired_at IS NULL
		ORDER BY n.display_name, i.vendor, i.natural_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Instance
	for rows.Next() {
		var in Instance
		var logs string
		if err := rows.Scan(&in.ID, &in.NodeID, &in.ClusterID, &in.Vendor, &in.NaturalKey,
			&in.DisplayName, &in.Version, &in.BinaryPath, &in.ConfigRoot, &in.MainConfigPath,
			&in.ServiceManager, &in.UnitName, &in.DetectedPID, &logs, &in.FirstSeenAt,
			&in.LastSeenAt, &in.RetiredAt, &in.NodeAddress, &in.NodeDisplayName, &in.SiteCount,
			&in.LastCaptured, &in.ClusterName, &in.Degraded, &in.ParseState,
			&in.Ports); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(logs), &in.AccessLogPaths)
		out = append(out, in)
	}
	return out, rows.Err()
}

func (db *DB) Instance(ctx context.Context, id int64) (Instance, error) {
	var in Instance
	var logs string
	err := db.R.QueryRowContext(ctx, `SELECT i.id, i.node_id, i.cluster_id, i.vendor,
		i.natural_key, i.display_name, i.version, i.binary_path, i.config_root,
		i.main_config_path, i.build_flags, i.service_manager, i.unit_name, i.detected_pid,
		i.access_log_paths, i.first_seen_at, i.last_seen_at, i.retired_at,
		n.address, n.display_name
		FROM instance i JOIN node n ON n.id = i.node_id WHERE i.id = ?`, id).
		Scan(&in.ID, &in.NodeID, &in.ClusterID, &in.Vendor, &in.NaturalKey, &in.DisplayName,
			&in.Version, &in.BinaryPath, &in.ConfigRoot, &in.MainConfigPath, &in.BuildFlags,
			&in.ServiceManager, &in.UnitName, &in.DetectedPID, &logs, &in.FirstSeenAt,
			&in.LastSeenAt, &in.RetiredAt, &in.NodeAddress, &in.NodeDisplayName)
	if err != nil {
		return in, err
	}
	json.Unmarshal([]byte(logs), &in.AccessLogPaths)
	return in, nil
}

// ------------------------------------------------------------- collections

func (db *DB) StartCollection(ctx context.Context, nodeID int64, trigger string, actor *int64) (int64, error) {
	res, err := db.W.ExecContext(ctx, `INSERT INTO collection
		(node_id, trigger, actor_user_id, started_at, status) VALUES (?,?,?,?, 'running')`,
		nodeID, trigger, actor, Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) FinishCollection(ctx context.Context, id int64, status, errMsg string, instances int, bytesStored int64, durationMS int64) error {
	_, err := db.W.ExecContext(ctx, `UPDATE collection SET finished_at = ?, status = ?,
		error = ?, instances_seen = ?, bytes_stored = ?, duration_ms = ? WHERE id = ?`,
		Now(), status, errMsg, instances, bytesStored, durationMS, id)
	if err != nil {
		return err
	}
	_, err = db.W.ExecContext(ctx,
		`UPDATE node SET last_collection_id = ? WHERE id = (SELECT node_id FROM collection WHERE id = ?)`,
		id, id)
	return err
}

type Collection struct {
	ID            int64
	NodeID        int64
	NodeName      string
	Trigger       string
	StartedAt     string
	FinishedAt    sql.NullString
	Status        string
	Error         string
	InstancesSeen int
	BytesStored   int64
	DurationMS    sql.NullInt64
}

func (db *DB) Collections(ctx context.Context, limit int) ([]Collection, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT c.id, c.node_id, n.display_name, c.trigger,
		c.started_at, c.finished_at, c.status, c.error, c.instances_seen, c.bytes_stored,
		c.duration_ms FROM collection c JOIN node n ON n.id = c.node_id
		ORDER BY c.started_at DESC, c.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		if err := rows.Scan(&c.ID, &c.NodeID, &c.NodeName, &c.Trigger, &c.StartedAt,
			&c.FinishedAt, &c.Status, &c.Error, &c.InstancesSeen, &c.BytesStored,
			&c.DurationMS); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --------------------------------------------------------------- snapshots

// SnapshotFile is one captured file on its way into the store.
type SnapshotFile struct {
	Kind      string // config_file | vendor_dump | log_format_sample
	Path      string
	Content   []byte
	Truncated bool
}

type Snapshot struct {
	ID             int64
	InstanceID     int64
	CollectionID   int64
	CapturedAt     string
	ConfigSource   string
	Degraded       bool
	DegradedReason string
	ContentSHA256  string
	FileCount      int
	BytesRaw       int64
	ParseState     string
	ParseError     string
	ParserVersion  sql.NullInt64
	IsCurrent      bool
}

// WriteSnapshot stores the files, computes the Snapshot digest over the file set
// (so an unchanged config yields an identical content_sha256), and makes it
// current. Everything happens in one transaction: a partial Snapshot must never
// become current, which is what makes crash recovery trivial.
func (db *DB) WriteSnapshot(ctx context.Context, snap Snapshot, files []SnapshotFile) (int64, error) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Kind != files[j].Kind {
			return files[i].Kind < files[j].Kind
		}
		return files[i].Path < files[j].Path
	})

	digests := make([]string, len(files))
	var bytesRaw int64
	for i, f := range files {
		d, err := db.PutBlob(ctx, f.Content)
		if err != nil {
			return 0, err
		}
		digests[i] = d
		bytesRaw += int64(len(f.Content))
	}

	h := sha256.New()
	for i, f := range files {
		h.Write([]byte(f.Kind + "\x00" + f.Path + "\x00" + digests[i] + "\n"))
	}
	snap.ContentSHA256 = hex.EncodeToString(h.Sum(nil))
	snap.FileCount = len(files)
	snap.BytesRaw = bytesRaw
	if snap.CapturedAt == "" {
		snap.CapturedAt = Now()
	}
	// captured_at is unique per Instance at one-second resolution, so two
	// collections inside the same second would collide. Step forward rather than
	// fail: losing a Snapshot to a double-clicked button is not acceptable.
	for {
		var n int
		if err := db.R.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM snapshot WHERE instance_id = ? AND captured_at = ?`,
			snap.InstanceID, snap.CapturedAt).Scan(&n); err != nil || n == 0 {
			break
		}
		t, err := time.Parse("2006-01-02T15:04:05Z", snap.CapturedAt)
		if err != nil {
			break
		}
		snap.CapturedAt = t.Add(time.Second).Format("2006-01-02T15:04:05Z")
	}
	if snap.ParseState == "" {
		snap.ParseState = "pending"
	}

	tx, err := db.W.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE snapshot SET is_current = 0 WHERE instance_id = ? AND is_current = 1`,
		snap.InstanceID); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO snapshot
		(collection_id, instance_id, captured_at, config_source, degraded, degraded_reason,
		 content_sha256, file_count, bytes_raw, parse_state, parse_error, parser_version, is_current)
		VALUES (?,?,?,?,?,?,?,?,?,?,'',NULL,1)`,
		snap.CollectionID, snap.InstanceID, snap.CapturedAt, snap.ConfigSource,
		boolInt(snap.Degraded), snap.DegradedReason, snap.ContentSHA256,
		snap.FileCount, snap.BytesRaw, snap.ParseState)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for i, f := range files {
		if _, err := tx.ExecContext(ctx, `INSERT INTO snapshot_file
			(snapshot_id, kind, path, blob_sha256, bytes_raw, truncated)
			VALUES (?,?,?,?,?,?)`,
			id, f.Kind, f.Path, digests[i], len(f.Content), boolInt(f.Truncated)); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (db *DB) CurrentSnapshot(ctx context.Context, instanceID int64) (Snapshot, error) {
	return scanSnapshot(db.R.QueryRowContext(ctx, `SELECT `+snapshotCols+`
		FROM snapshot WHERE instance_id = ? AND is_current = 1`, instanceID))
}

func (db *DB) SnapshotByID(ctx context.Context, id int64) (Snapshot, error) {
	return scanSnapshot(db.R.QueryRowContext(ctx,
		`SELECT `+snapshotCols+` FROM snapshot WHERE id = ?`, id))
}

const snapshotCols = `id, instance_id, collection_id, captured_at, config_source, degraded,
	degraded_reason, content_sha256, file_count, bytes_raw, parse_state, parse_error,
	parser_version, is_current`

func scanSnapshot(sc interface{ Scan(...any) error }) (Snapshot, error) {
	var s Snapshot
	err := sc.Scan(&s.ID, &s.InstanceID, &s.CollectionID, &s.CapturedAt, &s.ConfigSource,
		&s.Degraded, &s.DegradedReason, &s.ContentSHA256, &s.FileCount, &s.BytesRaw,
		&s.ParseState, &s.ParseError, &s.ParserVersion, &s.IsCurrent)
	return s, err
}

func (db *DB) Snapshots(ctx context.Context, instanceID int64, limit int) ([]Snapshot, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT `+snapshotCols+` FROM snapshot
		WHERE instance_id = ? ORDER BY captured_at DESC LIMIT ?`, instanceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type FileRef struct {
	ID        int64
	Kind      string
	Path      string
	Digest    string
	BytesRaw  int64
	Truncated bool
}

func (db *DB) SnapshotFiles(ctx context.Context, snapshotID int64) ([]FileRef, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT id, kind, path, blob_sha256, bytes_raw, truncated
		FROM snapshot_file WHERE snapshot_id = ? ORDER BY kind, path`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FileRef
	for rows.Next() {
		var f FileRef
		if err := rows.Scan(&f.ID, &f.Kind, &f.Path, &f.Digest, &f.BytesRaw, &f.Truncated); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (db *DB) SetParseState(ctx context.Context, snapshotID int64, state, errMsg string) error {
	_, err := db.W.ExecContext(ctx,
		`UPDATE snapshot SET parse_state = ?, parse_error = ?, parser_version = ? WHERE id = ?`,
		state, errMsg, ParserVersion, snapshotID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
