package web

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/nagiflow/nagipath/internal/license"
	"github.com/nagiflow/nagipath/internal/store"
)

// ---------------------------------------------------------------- diagnostics

// diagnosticsPage is what diagnostics.html renders: docs/frontend/settings.md
// §10's read-only panel, minus scheduler state and worker pool utilisation —
// neither corresponds to anything real in this codebase (collection is one
// serial ticker goroutine, and the job table is schema-only) — and minus a
// separate "build commit" field, since Version is already a `git describe`
// output that carries the commit whenever it isn't a clean tag.
type diagnosticsPage struct {
	Version   string
	GoVersion string
	Uptime    time.Duration

	DBPath      string
	DBSizeBytes int64

	MasterKeyPath    string
	MasterKeyPresent bool
	MasterKeyMode    string // e.g. "0600"; empty unless MasterKeyPresent

	ListenAddr string
	TLSEnabled bool
	DemoMode   bool

	LicenseStatus  license.Status
	LicenseMessage string

	MigrationsApplied  int
	MigrationsExpected int
}

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := diagnosticsPage{
		Version:       Version,
		GoVersion:     runtime.Version(),
		Uptime:        time.Since(s.StartedAt).Round(time.Second),
		DBPath:        s.DB.Path,
		ListenAddr:    s.ListenAddr,
		TLSEnabled:    s.TLSEnabled,
		DemoMode:      s.DemoMode,
		MasterKeyPath: s.Master.Path,
	}
	if fi, err := os.Stat(s.DB.Path); err == nil {
		d.DBSizeBytes = fi.Size()
	}
	if fi, err := os.Stat(s.Master.Path); err == nil {
		d.MasterKeyPresent = true
		d.MasterKeyMode = fmt.Sprintf("%#o", fi.Mode().Perm())
	}
	d.LicenseStatus, d.LicenseMessage = s.licenseStatus(ctx)
	applied, err := s.DB.AppliedMigrations(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	d.MigrationsApplied = len(applied)
	d.MigrationsExpected, _ = store.ExpectedMigrationCount()
	// "System", not "Diagnostics": that is what the settings nav and the page heading
	// call it, and the breadcrumb was the one place using the file's name instead.
	s.render(w, r, "diagnostics.html", "System", d)
}

// diagnosticsBundle is the same facts as diagnostics.html, as one plain-text
// download rather than a zip — there's nothing here that needs more than one
// file. It never touches credentials, Master Key contents, session/token
// values, certificate material, Probe tokens, or collected configuration file
// contents; the exclusion list on the page states that up front, before the
// download link, so whoever is about to email this bundle out can see exactly
// what it does and doesn't contain.
func (s *Server) diagnosticsBundle(w http.ResponseWriter, r *http.Request) {
	ctx, u := r.Context(), userOf(r)
	var b strings.Builder

	now := time.Now().UTC()
	fmt.Fprintf(&b, "nagipath diagnostics bundle\ngenerated %s\n\n", now.Format(time.RFC3339))

	fmt.Fprintf(&b, "== Version ==\nnagipath %s\nGo %s\n\n", Version, runtime.Version())

	fmt.Fprintf(&b, "== Uptime ==\n%s (started %s)\n\n",
		time.Since(s.StartedAt).Round(time.Second), s.StartedAt.UTC().Format(time.RFC3339))

	fmt.Fprintf(&b, "== Database ==\npath: %s\n", s.DB.Path)
	if fi, err := os.Stat(s.DB.Path); err == nil {
		fmt.Fprintf(&b, "size: %d bytes\n", fi.Size())
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "== Configuration ==\nlisten address: %s\nTLS enabled: %v\ndemo mode: %v\n\n",
		s.ListenAddr, s.TLSEnabled, s.DemoMode)

	b.WriteString("== Collection statistics ==\n")
	statusCounts, _ := s.DB.CollectionStatusCounts(ctx)
	for _, status := range []string{"succeeded", "degraded", "failed", "running"} {
		fmt.Fprintf(&b, "%s: %d\n", status, statusCounts[status])
	}
	durSum, durCount, _ := s.DB.CollectionDurationStats(ctx)
	fmt.Fprintf(&b, "duration_ms: sum=%d count=%d\n\n", durSum, durCount)

	applied, _ := s.DB.AppliedMigrations(ctx)
	expected, _ := store.ExpectedMigrationCount()
	fmt.Fprintf(&b, "== Migrations ==\napplied %d of %d expected\n", len(applied), expected)
	for _, name := range applied {
		fmt.Fprintf(&b, "  %s\n", name)
	}
	b.WriteString("\n")

	status, message := s.licenseStatus(ctx)
	fmt.Fprintf(&b, "== License ==\nstatus: %s\n%s\n\n", status, message)

	// No redaction pass runs over these lines: the codebase's own logging
	// discipline already guarantees no key material, session/token value or
	// Probe token is ever written to the log in the first place
	// (docs/infra/customer_deployment.md's Logs section), so there is nothing
	// secret in this stream to strip before it goes into the bundle.
	b.WriteString("== Recent logs ==\n")
	if s.Logs != nil {
		for _, line := range s.Logs.Lines() {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")

	b.WriteString("== Excluded from this bundle ==\n")
	b.WriteString("credentials, Master Key contents, session/token values, certificate " +
		"material, Probe tokens, collected configuration file contents\n")

	filename := "nagipath-diagnostics-" + strings.ReplaceAll(now.Format(time.RFC3339), ":", "-") + ".txt"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write([]byte(b.String()))

	// The download has already gone out; a failure to record it is logged, not
	// surfaced, matching installLicense's own non-fatal audit write.
	if err := s.DB.Audit(ctx, &u.ID, "diagnostics.download", "diagnostics", nil, ""); err != nil {
		s.Log.Warn("could not record diagnostics.download audit entry", "err", err)
	}
}
