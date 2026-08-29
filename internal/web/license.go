package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nagiflow/nagipath/internal/license"
)

// ---------------------------------------------------------------- license

// installLicenseBytes is internal/api's postInstallLicense hook (wired via
// api.Server.InstallLicense in web.go's New): parse, persist to disk, swap
// the in-memory pointer and record license_state — everything installLicense
// used to do inline, minus the audit entry, which the caller writes with the
// actor it already has from the request context.
func (s *Server) installLicenseBytes(ctx context.Context, raw []byte, by *int64) (*license.License, error) {
	lic, err := license.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := writeLicenseFile(s.LicensePath, raw); err != nil {
		return nil, fmt.Errorf("license verified but could not be saved to disk: %w", err)
	}
	s.setLicense(lic)
	if err := s.DB.UpsertLicenseState(ctx, string(raw), lic.Customer, lic.Edition, lic.NodeCeiling, lic.Expiry, true, by); err != nil {
		s.Log.Error("could not record license_state", "err", err)
	}
	return lic, nil
}

// writeLicenseFile persists a newly pasted license so it survives a restart,
// via temp-file-then-rename: a crash mid-write must never leave a torn,
// unparseable license file where the server expects to find one on next boot.
func writeLicenseFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".license-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename below has succeeded
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
