package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// Content-addressed storage. Two Snapshots of an unchanged config file share one
// blob, which is what makes hourly collection across a large fleet affordable.
//
// Retention deletes snapshot_file rows first, then garbage-collects blobs that
// no row references. Doing it the other way round corrupts every Snapshot that
// shared the content.

var (
	zEnc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zDec, _ = zstd.NewReader(nil)
)

// PutBlob stores content if it is not already present and returns its digest.
func (db *DB) PutBlob(ctx context.Context, content []byte) (string, error) {
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	var exists int
	err := db.R.QueryRowContext(ctx, `SELECT 1 FROM blob WHERE sha256 = ?`, digest).Scan(&exists)
	if err == nil {
		return digest, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	packed := zEnc.EncodeAll(content, nil)
	_, err = db.W.ExecContext(ctx, `INSERT OR IGNORE INTO blob
		(sha256, bytes_raw, bytes_zstd, content, created_at) VALUES (?,?,?,?,?)`,
		digest, len(content), len(packed), packed, Now())
	if err != nil {
		return "", err
	}
	return digest, nil
}

func (db *DB) Blob(ctx context.Context, digest string) ([]byte, error) {
	var packed []byte
	var raw int
	if err := db.R.QueryRowContext(ctx,
		`SELECT content, bytes_raw FROM blob WHERE sha256 = ?`, digest).Scan(&packed, &raw); err != nil {
		return nil, err
	}
	out, err := zDec.DecodeAll(packed, make([]byte, 0, raw))
	if err != nil {
		return nil, fmt.Errorf("blob %s is corrupt: %w", digest[:12], err)
	}
	return out, nil
}

// GCBlobs removes blobs no snapshot_file references. Safe to run any time;
// called after retention.
func (db *DB) GCBlobs(ctx context.Context) (int64, error) {
	res, err := db.W.ExecContext(ctx, `DELETE FROM blob WHERE sha256 NOT IN
		(SELECT blob_sha256 FROM snapshot_file)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
