package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// LicenseState is the single-row record of the most recently installed
// license (license_state, id=1) — what an admin last pasted in, distinct from
// whatever License the running process currently has loaded (which can also
// come from NAGIPATH_LICENSE_FILE at startup). See internal/web's license
// page for how the two are shown side by side.
type LicenseState struct {
	LicenseBlob         string
	Customer            string
	Edition             string
	NodeCeiling         int
	ExpiresAt           string
	SignatureValid      bool
	LastEvaluatedAt     string
	InstalledBy         sql.NullInt64
	InstalledByUsername string // "" if InstalledBy is unset or the user was since deleted
}

// UpsertLicenseState records the outcome of installing (or re-evaluating) a
// license. There is only ever one row: a new license replaces the record of
// the last one, it does not accumulate history (audit_event already carries
// the "license.install" trail).
func (db *DB) UpsertLicenseState(ctx context.Context, blob string, customer, edition string, nodeCeiling int, expiresAt time.Time, signatureValid bool, installedBy *int64) error {
	_, err := db.W.ExecContext(ctx, `INSERT INTO license_state
		(id, license_blob, customer, edition, node_ceiling, expires_at, signature_valid, last_evaluated_at, installed_by)
		VALUES (1,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			license_blob = excluded.license_blob, customer = excluded.customer,
			edition = excluded.edition, node_ceiling = excluded.node_ceiling,
			expires_at = excluded.expires_at, signature_valid = excluded.signature_valid,
			last_evaluated_at = excluded.last_evaluated_at, installed_by = excluded.installed_by`,
		// expires_at uses the same ISO-8601-with-Z form as every other TEXT
		// timestamp column (Now(), above) rather than license.go's bare-date
		// layout, so it sorts and compares the same way user_session.expires_at
		// does — this column just happens to always land on a day boundary.
		blob, customer, edition, nodeCeiling, expiresAt.UTC().Format("2006-01-02T15:04:05Z"), signatureValid, Now(), installedBy)
	return err
}

// LicenseState returns the last-installed license record, or nil, nil if
// nothing has ever been installed through the UI — that is a normal state
// (e.g. a license supplied only via NAGIPATH_LICENSE_FILE), not an error.
func (db *DB) LicenseState(ctx context.Context) (*LicenseState, error) {
	var st LicenseState
	var installedByUsername sql.NullString
	err := db.R.QueryRowContext(ctx, `SELECT ls.license_blob, ls.customer, ls.edition, ls.node_ceiling,
		ls.expires_at, ls.signature_valid, ls.last_evaluated_at, ls.installed_by, u.username
		FROM license_state ls LEFT JOIN app_user u ON u.id = ls.installed_by
		WHERE ls.id = 1`).Scan(&st.LicenseBlob, &st.Customer, &st.Edition, &st.NodeCeiling,
		&st.ExpiresAt, &st.SignatureValid, &st.LastEvaluatedAt, &st.InstalledBy, &installedByUsername)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st.InstalledByUsername = installedByUsername.String
	return &st, nil
}
