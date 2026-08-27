package web

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/store"
)

// ---------------------------------------------------------------- license

// licensePage is what license.html renders: the License actually loaded into
// this process right now (which can come from NAGIPATH_LICENSE_FILE and so
// differ from the last one installed through the UI) alongside the
// license_state row for that last UI install, if any.
type licensePage struct {
	Loaded      bool // false when no License is currently loaded at all
	Customer    string
	Edition     string
	NodeCeiling int
	Expiry      time.Time
	Status      license.Status
	Message     string
	NodeCount   int
	State       *store.LicenseState
}

func (s *Server) license(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lic := s.currentLicense()
	status, message := s.licenseStatus(ctx)
	n, err := s.DB.NodeCount(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	state, err := s.DB.LicenseState(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := licensePage{Status: status, Message: message, NodeCount: n, State: state}
	if lic != nil {
		data.Loaded = true
		data.Customer, data.Edition, data.NodeCeiling, data.Expiry = lic.Customer, lic.Edition, lic.NodeCeiling, lic.Expiry
	}
	s.render(w, r, "license.html", "License", data)
}

func (s *Server) installLicense(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	raw := []byte(r.FormValue("license_text"))
	lic, err := license.Parse(raw)
	if err != nil {
		redirect(w, r, "/license", "", err.Error())
		return
	}
	if err := writeLicenseFile(s.LicensePath, raw); err != nil {
		redirect(w, r, "/license", "", "license verified but could not be saved to disk: "+err.Error())
		return
	}
	s.setLicense(lic)
	if err := s.DB.UpsertLicenseState(ctx, string(raw), lic.Customer, lic.Edition, lic.NodeCeiling, lic.Expiry, true, &u.ID); err != nil {
		s.Log.Error("could not record license_state", "err", err)
	}
	// Customer name only — never the raw license text or its signature — matching
	// how credential.create audits by name, not key material.
	s.DB.Audit(ctx, &u.ID, "license.install", "license", nil, lic.Customer)
	redirect(w, r, "/license", "license installed", "")
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
