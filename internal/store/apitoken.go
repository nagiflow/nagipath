package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"
)

var ErrInvalidAPIToken = errors.New("invalid API token")

type APIToken struct {
	ID         int64
	Name       string
	UserID     int64
	Username   string
	CreatedAt  string
	LastUsedAt sql.NullString
	ExpiresAt  sql.NullString
	RevokedAt  sql.NullString
	Prefix     string
}

func (db *DB) APITokens(ctx context.Context) ([]APIToken, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT t.id, t.name, t.user_id, u.username,
		t.created_at, t.last_used_at, t.expires_at, t.revoked_at, COALESCE(t.prefix, '')
		FROM api_token t JOIN app_user u ON u.id = t.user_id
		ORDER BY t.created_at DESC, t.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.UserID, &t.Username, &t.CreatedAt,
			&t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt, &t.Prefix); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CreateAPIToken returns the plaintext exactly once. Only its SHA-256 is stored.
func (db *DB) CreateAPIToken(ctx context.Context, name string, userID int64, lifetime time.Duration) (int64, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return 0, "", err
	}
	raw := "np_" + base64.RawURLEncoding.EncodeToString(random)
	sum := sha256.Sum256([]byte(raw))
	// Store the first 12 characters as the prefix for display
	prefix := raw
	if len(raw) > 12 {
		prefix = raw[:12]
	}
	var expires any
	if lifetime > 0 {
		expires = time.Now().UTC().Add(lifetime).Format("2006-01-02T15:04:05Z")
	}
	res, err := db.W.ExecContext(ctx, `INSERT INTO api_token
		(name, token_hash, prefix, user_id, created_at, expires_at) VALUES (?,?,?,?,?,?)`,
		name, hex.EncodeToString(sum[:]), prefix, userID, Now(), expires)
	if err != nil {
		return 0, "", err
	}
	id, err := res.LastInsertId()
	return id, raw, err
}

func (db *DB) RevokeAPIToken(ctx context.Context, id int64) error {
	res, err := db.W.ExecContext(ctx,
		`UPDATE api_token SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, Now(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// AuthenticateAPIToken verifies an active token and lazily records use. The
// minute guard keeps a busy automation job from turning every read into a write.
func (db *DB) AuthenticateAPIToken(ctx context.Context, raw string) (User, error) {
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	u, err := scanUser(db.R.QueryRowContext(ctx, `SELECT `+userColsU+`
		FROM api_token t JOIN app_user u ON u.id = t.user_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL
		  AND (t.expires_at IS NULL OR t.expires_at > ?)
		  AND u.disabled_at IS NULL`, hash, Now()))
	if err != nil {
		return User{}, ErrInvalidAPIToken
	}
	cutoff := time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05Z")
	_, _ = db.W.ExecContext(ctx, `UPDATE api_token SET last_used_at = ?
		WHERE token_hash = ? AND (last_used_at IS NULL OR last_used_at < ?)`, Now(), hash, cutoff)
	return u, nil
}
