// Package license implements ADR-0014: an offline, ed25519-signed license file
// verified against a public key embedded in the binary. There is no
// phone-home, no activation server and no network call anywhere in this
// package — verification is a signature check against local bytes.
//
// Enforcement is deliberately soft (ADR-0014). Nothing in this package can
// stop Collection, refuse to start the server or delete data. Load reports
// whether a license is present and valid; Status reports whether the fleet
// is within its terms. Both are advisory. The caller is responsible for
// turning a non-valid Status into a warning, a banner and an audit entry —
// never into a hard gate.
//
// # File format
//
// A license file is exactly two lines:
//
//	customer|node_ceiling|expiry|edition
//	<base64 standard-encoding ed25519 signature of line 1, without its newline>
//
// customer is a free-text name and must not contain "|". node_ceiling is a
// decimal integer. expiry is a date in "2006-01-02" form (the license is
// valid through the end of that day). edition is a short free-text label
// (e.g. "standard"). The signature is computed over line 1's exact bytes,
// excluding the trailing newline.
//
// This four-field pipe-delimited line was chosen over JSON so that the bytes
// signed are unambiguous without a canonicalization step — there is exactly
// one way to write four fields in a fixed order, which is all this record
// needs.
//
// Nothing in this package, or anywhere else in nagipath, can mint a license
// from the running binary. That capability is deliberately absent: a
// customer must not be able to self-issue. Licenses are produced by a
// separate, offline signing process nagiflow controls, using the private key
// that pairs with PublicKey below.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// PublicKey verifies license files shipped to customers.
//
// PLACEHOLDER: this is a throwaway keypair generated for development. The
// matching private key exists only in license_test.go, used solely to sign
// test fixtures. This public key MUST be replaced with nagiflow's real
// signing key before any customer release — every license file issued
// against the placeholder key becomes worthless the moment it is swapped.
var PublicKey = ed25519.PublicKey{
	0x9e, 0x07, 0x43, 0x07, 0x2d, 0xc4, 0x50, 0xbe, 0x37, 0xce, 0x15, 0x7d, 0xc1, 0x4f, 0x9b, 0x23,
	0x6c, 0x8b, 0xd0, 0xf6, 0x89, 0x56, 0x72, 0x3b, 0xd6, 0x46, 0x8c, 0xaf, 0xdc, 0x7e, 0xf3, 0xc7,
}

// expiryLayout is the date-only form used in the license file: no time, no
// zone, so a license is unambiguous no matter what time zone it is read in.
const expiryLayout = "2006-01-02"

// License is the plaintext record encoded in a license file.
type License struct {
	Customer    string
	NodeCeiling int
	Expiry      time.Time // truncated to a day; valid through 23:59:59 of that day
	Edition     string
}

// Status is the outcome of checking a License against the current fleet.
// Every value except Valid is a soft-enforcement state: the caller warns,
// it never stops anything.
type Status string

const (
	Valid       Status = "valid"
	Missing     Status = "missing"
	Expired     Status = "expired"
	OverCeiling Status = "over_ceiling"
)

// Message is the human-readable warning shown in the API header value's
// companion banner and logged at startup. It intentionally says nothing
// about enforcement, because there is none to describe.
func (s Status) Message() string {
	switch s {
	case Valid:
		return ""
	case Missing:
		return "No license file found. nagipath is running unlicensed."
	case Expired:
		return "The license has expired. nagipath keeps working; renew the license to clear this warning."
	case OverCeiling:
		return "The fleet has more Nodes than the license allows. nagipath keeps working; contact nagiflow to raise the ceiling."
	default:
		return "License status unknown."
	}
}

// Status reports whether nodeCount and now are within the License's terms.
// Expiry and over-ceiling can both be true at once; expiry is reported first
// since a lapsed term is the more fundamental problem of the two.
func (l *License) Status(nodeCount int, now time.Time) Status {
	if now.After(l.Expiry) {
		return Expired
	}
	if l.NodeCeiling > 0 && nodeCount > l.NodeCeiling {
		return OverCeiling
	}
	return Valid
}

// Load reads and verifies a license file at path. A missing file, a bad
// signature or a malformed record are all reported as an error — the caller
// decides how to treat that (ADR-0014: never as a reason to stop).
func Load(path string) (*License, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse verifies and decodes a license file's raw bytes, whether they came
// from disk (Load) or were pasted into the UI. A missing signature, a bad
// signature or a malformed record are all reported as an error — the caller
// decides how to treat that (ADR-0014: never as a reason to stop).
func Parse(raw []byte) (*License, error) {
	lines := strings.SplitN(strings.TrimRight(string(raw), "\n"), "\n", 2)
	if len(lines) != 2 {
		return nil, errors.New("license file is malformed: expected a record line and a signature line")
	}
	record, sigLine := lines[0], strings.TrimSpace(lines[1])

	sig, err := base64.StdEncoding.DecodeString(sigLine)
	if err != nil {
		return nil, fmt.Errorf("license signature is not valid base64: %w", err)
	}
	if !ed25519.Verify(PublicKey, []byte(record), sig) {
		return nil, errors.New("license signature does not verify against the embedded public key")
	}

	return parseRecord(record)
}

func parseRecord(record string) (*License, error) {
	fields := strings.SplitN(record, "|", 4)
	if len(fields) != 4 {
		return nil, fmt.Errorf("license record has %d field(s), want 4 (customer|node_ceiling|expiry|edition)", len(fields))
	}
	customer, ceilingRaw, expiryRaw, edition := fields[0], fields[1], fields[2], fields[3]
	if customer == "" {
		return nil, errors.New("license record has an empty customer field")
	}
	ceiling, err := strconv.Atoi(ceilingRaw)
	if err != nil {
		return nil, fmt.Errorf("license node ceiling %q is not an integer: %w", ceilingRaw, err)
	}
	expiry, err := time.Parse(expiryLayout, expiryRaw)
	if err != nil {
		return nil, fmt.Errorf("license expiry %q is not a %s date: %w", expiryRaw, expiryLayout, err)
	}
	return &License{Customer: customer, NodeCeiling: ceiling, Expiry: expiry, Edition: edition}, nil
}
