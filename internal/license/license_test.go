package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testSeed is the private half of the throwaway keypair whose public half is
// embedded in license.go as PublicKey. It exists only here, to sign license
// fixtures for tests. Nothing outside this file may use it: the running
// binary must never be able to mint its own license.
var testSeed = []byte{
	0x13, 0x8b, 0x26, 0x11, 0x7a, 0x70, 0x73, 0x6c, 0x71, 0xa0, 0x83, 0xff, 0x26, 0x74, 0x5b, 0x4c,
	0x50, 0x8a, 0xa2, 0xc0, 0xb0, 0x6a, 0xb6, 0x3d, 0x2b, 0x5a, 0xab, 0x45, 0xff, 0xd0, 0xd2, 0x59,
}

func testPrivateKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(testSeed)
	if !priv.Public().(ed25519.PublicKey).Equal(PublicKey) {
		t.Fatal("testSeed does not match the embedded PublicKey; regenerate both together")
	}
	return priv
}

// signFixture builds a license file's bytes for the given record line,
// signed with the test private key.
func signFixture(t *testing.T, record string) []byte {
	t.Helper()
	priv := testPrivateKey(t)
	sig := ed25519.Sign(priv, []byte(record))
	return []byte(record + "\n" + base64.StdEncoding.EncodeToString(sig) + "\n")
}

func validRecord(expiry time.Time) string {
	return "Acme Corp|50|" + expiry.Format(expiryLayout) + "|standard"
}

func writeFixture(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "license.lic")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRoundTrip(t *testing.T) {
	expiry := time.Now().Add(365 * 24 * time.Hour)
	record := validRecord(expiry)
	path := writeFixture(t, signFixture(t, record))

	lic, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if lic.Customer != "Acme Corp" {
		t.Errorf("Customer = %q, want %q", lic.Customer, "Acme Corp")
	}
	if lic.NodeCeiling != 50 {
		t.Errorf("NodeCeiling = %d, want 50", lic.NodeCeiling)
	}
	if lic.Edition != "standard" {
		t.Errorf("Edition = %q, want %q", lic.Edition, "standard")
	}
	if lic.Expiry.Format(expiryLayout) != expiry.Format(expiryLayout) {
		t.Errorf("Expiry = %v, want date %s", lic.Expiry, expiry.Format(expiryLayout))
	}
}

func TestLoadTamperedSignature(t *testing.T) {
	record := validRecord(time.Now().Add(24 * time.Hour))
	contents := signFixture(t, record)

	// Flip a byte inside the base64 signature line.
	lines := strings.SplitN(strings.TrimRight(string(contents), "\n"), "\n", 2)
	sig, err := base64.StdEncoding.DecodeString(lines[1])
	if err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 0xFF
	tampered := lines[0] + "\n" + base64.StdEncoding.EncodeToString(sig) + "\n"
	path := writeFixture(t, []byte(tampered))

	if _, err := Load(path); err == nil {
		t.Error("Load accepted a tampered signature")
	}
}

func TestLoadTamperedRecord(t *testing.T) {
	record := validRecord(time.Now().Add(24 * time.Hour))
	contents := signFixture(t, record)
	lines := strings.SplitN(strings.TrimRight(string(contents), "\n"), "\n", 2)
	tampered := lines[0] + "-tampered\n" + lines[1] + "\n"
	path := writeFixture(t, []byte(tampered))

	if _, err := Load(path); err == nil {
		t.Error("Load accepted a record that does not match its signature")
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.lic")); err == nil {
		t.Error("Load succeeded on a missing file")
	}
}

func TestLoadMalformedRecord(t *testing.T) {
	cases := []string{
		"only|three|fields",
		"Acme|not-a-number|2030-01-01|standard",
		"Acme|50|not-a-date|standard",
		"|50|2030-01-01|standard",
	}
	for _, record := range cases {
		t.Run(record, func(t *testing.T) {
			path := writeFixture(t, signFixture(t, record))
			if _, err := Load(path); err == nil {
				t.Errorf("Load accepted malformed record %q", record)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		lic       License
		nodeCount int
		want      Status
	}{
		{
			name:      "valid",
			lic:       License{NodeCeiling: 10, Expiry: now.Add(24 * time.Hour)},
			nodeCount: 5,
			want:      Valid,
		},
		{
			name:      "expired",
			lic:       License{NodeCeiling: 10, Expiry: now.Add(-time.Hour)},
			nodeCount: 5,
			want:      Expired,
		},
		{
			name:      "over ceiling",
			lic:       License{NodeCeiling: 10, Expiry: now.Add(24 * time.Hour)},
			nodeCount: 11,
			want:      OverCeiling,
		},
		{
			name:      "expired takes priority over over-ceiling",
			lic:       License{NodeCeiling: 10, Expiry: now.Add(-time.Hour)},
			nodeCount: 999,
			want:      Expired,
		},
		{
			name:      "at ceiling is not over",
			lic:       License{NodeCeiling: 10, Expiry: now.Add(24 * time.Hour)},
			nodeCount: 10,
			want:      Valid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.lic.Status(tc.nodeCount, now)
			if got != tc.want {
				t.Errorf("Status = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMessageNonEmptyForNonValid(t *testing.T) {
	for _, s := range []Status{Missing, Expired, OverCeiling} {
		if s.Message() == "" {
			t.Errorf("%s.Message() is empty, want a human-readable warning", s)
		}
	}
	if Valid.Message() != "" {
		t.Errorf("Valid.Message() = %q, want empty", Valid.Message())
	}
}
