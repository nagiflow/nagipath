package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var ErrAuth = errors.New("invalid username or password")

type User struct {
	ID                 int64
	Username           string
	Role               string
	DisplayName        string
	MustChangePassword bool
	CreatedAt          string
	LastLoginAt        sql.NullString
	Disabled           bool
}

func (u User) IsAdmin() bool { return u.Role == "admin" }

// Argon2id parameters. 64 MiB and one pass is the RFC 9106 second recommended
// option: enough to make an offline attack expensive without making a login on a
// small VM take a noticeable moment.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword is constant-time in the comparison. A malformed stored hash is a
// verification failure, never a pass.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func (db *DB) CreateUser(ctx context.Context, username, password, role, displayName string, mustChange bool) (int64, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return 0, err
	}
	res, err := db.W.ExecContext(ctx, `INSERT INTO app_user
		(username, password_hash, role, display_name, must_change_password, created_at)
		VALUES (?,?,?,?,?,?)`,
		username, hash, role, displayName, boolInt(mustChange), Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const userCols = `id, username, role, display_name, must_change_password, created_at,
	last_login_at, disabled_at IS NOT NULL`

const userColsU = `u.id, u.username, u.role, u.display_name, u.must_change_password,
	u.created_at, u.last_login_at, u.disabled_at IS NOT NULL`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var u User
	err := sc.Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName, &u.MustChangePassword,
		&u.CreatedAt, &u.LastLoginAt, &u.Disabled)
	return u, err
}

func (db *DB) User(ctx context.Context, id int64) (User, error) {
	return scanUser(db.R.QueryRowContext(ctx,
		`SELECT `+userCols+` FROM app_user WHERE id = ?`, id))
}

func (db *DB) Users(ctx context.Context) ([]User, error) {
	rows, err := db.R.QueryContext(ctx, `SELECT `+userCols+` FROM app_user ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_user`).Scan(&n)
	return n, err
}

// Authenticate verifies a password. A missing user still costs one hash so the
// response time does not reveal which usernames exist.
func (db *DB) Authenticate(ctx context.Context, username, password string) (User, error) {
	var u User
	var hash string
	err := db.R.QueryRowContext(ctx, `SELECT `+userCols+`, password_hash
		FROM app_user WHERE username = ?`, username).Scan(&u.ID, &u.Username, &u.Role,
		&u.DisplayName, &u.MustChangePassword, &u.CreatedAt, &u.LastLoginAt, &u.Disabled, &hash)
	if err == sql.ErrNoRows {
		HashPassword(password)
		return User{}, ErrAuth
	}
	if err != nil {
		return User{}, err
	}
	if u.Disabled || !VerifyPassword(hash, password) {
		return User{}, ErrAuth
	}
	if _, err := db.W.ExecContext(ctx,
		`UPDATE app_user SET last_login_at = ? WHERE id = ?`, Now(), u.ID); err != nil {
		return User{}, err
	}
	return u, nil
}

func (db *DB) SetPassword(ctx context.Context, id int64, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = db.W.ExecContext(ctx,
		`UPDATE app_user SET password_hash = ?, must_change_password = 0 WHERE id = ?`, hash, id)
	return err
}

func (db *DB) SetUserDisabled(ctx context.Context, id int64, disabled bool) error {
	var at any
	if disabled {
		at = Now()
	}
	_, err := db.W.ExecContext(ctx, `UPDATE app_user SET disabled_at = ? WHERE id = ?`, at, id)
	return err
}

// EnabledAdminCount counts admins who are not disabled, excluding excludeID (0
// excludes nothing, since ids start at 1). It is the last-admin guardrail: a
// caller about to disable an admin passes that admin's own id and refuses the
// action if the result is zero, so the product can never be left with no
// account able to sign a new admin in.
func (db *DB) EnabledAdminCount(ctx context.Context, excludeID int64) (int, error) {
	var n int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_user
		WHERE role = 'admin' AND disabled_at IS NULL AND id != ?`, excludeID).Scan(&n)
	return n, err
}

// LoginRateLimited enforces a per-username guardrail against sustained online
// password guessing. It counts failed attempts already written to audit_event
// by postLogin — the same "query the rows a mutation already writes" approach
// probe.RateLimited uses against the probe table — rather than adding a new
// table just to count logins.
//
// A limit of 0 disables the guardrail, the same convention as the Probe rate
// limit (probe.RateLimited): an operator who sets it to 0 is turning the check
// off, not setting it to "never".
func (db *DB) LoginRateLimited(ctx context.Context, username string) (bool, error) {
	limit := db.SettingInt(ctx, "login_rate_limit_max")
	if limit <= 0 {
		return false, nil
	}
	window := db.SettingInt(ctx, "login_rate_limit_window_seconds")
	since := time.Now().UTC().Add(-time.Duration(window) * time.Second).Format("2006-01-02T15:04:05Z")
	var n int
	err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_event
		WHERE action = 'auth.login' AND outcome = 'failure' AND target_label = ? AND at >= ?`,
		username, since).Scan(&n)
	if err != nil {
		return false, err
	}
	return n >= limit, nil
}

// ---------------------------------------------------------------- sessions

const sessionTTL = 12 * time.Hour

// NewSession returns the cookie value. Only its SHA-256 is stored, so a stolen
// database gives no usable session tokens.
func (db *DB) NewSession(ctx context.Context, userID int64, userAgent, remoteAddr string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().UTC().Add(sessionTTL).Format("2006-01-02T15:04:05Z")
	if _, err := db.W.ExecContext(ctx, `INSERT INTO user_session
		(token_hash, user_id, created_at, expires_at, user_agent, remote_addr)
		VALUES (?,?,?,?,?,?)`,
		hashToken(token), userID, Now(), expires, userAgent, remoteAddr); err != nil {
		return "", err
	}
	return token, nil
}

func (db *DB) SessionUser(ctx context.Context, token string) (User, error) {
	var u User
	err := db.R.QueryRowContext(ctx, `SELECT `+userColsU+`
		FROM user_session s JOIN app_user u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled_at IS NULL`,
		hashToken(token), Now()).Scan(&u.ID, &u.Username, &u.Role, &u.DisplayName,
		&u.MustChangePassword, &u.CreatedAt, &u.LastLoginAt, &u.Disabled)
	return u, err
}

func (db *DB) EndSession(ctx context.Context, token string) error {
	_, err := db.W.ExecContext(ctx,
		`DELETE FROM user_session WHERE token_hash = ?`, hashToken(token))
	return err
}

// EndAllSessions is what a password change must call: an old password should not
// keep an old browser logged in.
func (db *DB) EndAllSessions(ctx context.Context, userID int64) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM user_session WHERE user_id = ?`, userID)
	return err
}

func (db *DB) ExpireSessions(ctx context.Context) error {
	_, err := db.W.ExecContext(ctx, `DELETE FROM user_session WHERE expires_at <= ?`, Now())
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
