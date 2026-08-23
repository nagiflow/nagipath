package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func licenseStateTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "license_state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestLicenseStateNoRowIsNilNotError pins down the "nothing installed yet"
// state: a license supplied only via NAGIPATH_LICENSE_FILE never touches this
// table, so an empty table has to be a normal, non-error state.
func TestLicenseStateNoRowIsNilNotError(t *testing.T) {
	db := licenseStateTestDB(t)
	st, err := db.LicenseState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st != nil {
		t.Fatalf("LicenseState on an empty table = %+v, want nil", st)
	}
}

// TestUpsertLicenseStateThenRead confirms the row round-trips, including the
// installed_by -> app_user join for a display name.
func TestUpsertLicenseStateThenRead(t *testing.T) {
	ctx := context.Background()
	db := licenseStateTestDB(t)

	adminID, err := db.CreateUser(ctx, "admin", "a good long password", "admin", "Admin", false)
	if err != nil {
		t.Fatal(err)
	}

	expiry := time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertLicenseState(ctx, "raw-blob-1", "Acme Corp", "standard", 50, expiry, true, &adminID); err != nil {
		t.Fatal(err)
	}

	st, err := db.LicenseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st == nil {
		t.Fatal("LicenseState after an upsert = nil, want the row")
	}
	if st.Customer != "Acme Corp" || st.Edition != "standard" || st.NodeCeiling != 50 {
		t.Errorf("LicenseState = %+v, want customer=Acme Corp edition=standard ceiling=50", st)
	}
	if st.ExpiresAt != "2027-01-15T00:00:00Z" {
		t.Errorf("ExpiresAt = %q, want 2027-01-15T00:00:00Z", st.ExpiresAt)
	}
	if !st.SignatureValid {
		t.Error("SignatureValid = false, want true")
	}
	if !st.InstalledBy.Valid || st.InstalledBy.Int64 != adminID {
		t.Errorf("InstalledBy = %+v, want %d", st.InstalledBy, adminID)
	}
	if st.InstalledByUsername != "admin" {
		t.Errorf("InstalledByUsername = %q, want admin", st.InstalledByUsername)
	}
}

// TestUpsertLicenseStateTwiceOverwrites confirms this is a single-row table:
// a second install replaces the record of the first rather than adding to it.
func TestUpsertLicenseStateTwiceOverwrites(t *testing.T) {
	ctx := context.Background()
	db := licenseStateTestDB(t)

	first := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertLicenseState(ctx, "raw-blob-1", "Acme Corp", "standard", 50, first, true, nil); err != nil {
		t.Fatal(err)
	}
	second := time.Date(2028, 3, 1, 0, 0, 0, 0, time.UTC)
	if err := db.UpsertLicenseState(ctx, "raw-blob-2", "Globex", "enterprise", 500, second, false, nil); err != nil {
		t.Fatal(err)
	}

	st, err := db.LicenseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st == nil {
		t.Fatal("LicenseState after two upserts = nil, want the latest row")
	}
	if st.Customer != "Globex" || st.Edition != "enterprise" || st.NodeCeiling != 500 {
		t.Errorf("LicenseState = %+v, want the second install's values", st)
	}
	if st.ExpiresAt != "2028-03-01T00:00:00Z" {
		t.Errorf("ExpiresAt = %q, want 2028-03-01T00:00:00Z", st.ExpiresAt)
	}
	if st.SignatureValid {
		t.Error("SignatureValid = true, want false (the second install's value)")
	}
	if st.InstalledBy.Valid {
		t.Error("InstalledBy should be unset when installedBy is nil")
	}

	var n int
	if err := db.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM license_state`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("license_state has %d row(s), want exactly 1", n)
	}
}
