package keys

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := Generate(path); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	m, err := Load("", path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	aad := AAD("credential", "private_key", 1)
	ct, nonce, err := m.Seal([]byte("hunter2"), aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	pt, err := m.Open(ct, nonce, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(pt) != "hunter2" {
		t.Errorf("Open = %q, want %q", pt, "hunter2")
	}
}

func TestGenerateRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := Generate(path); err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	err := Generate(path)
	if err == nil {
		t.Fatal("second Generate over an existing file succeeded")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error = %q, want it to mention %q", err.Error(), "already exists")
	}
}

func TestLoadMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.key")
	_, err := Load("", path)
	if !errors.Is(err, ErrMissing) {
		t.Errorf("err = %v, want errors.Is(err, ErrMissing)", err)
	}
}

func TestLoadNoValueAndNoPath(t *testing.T) {
	_, err := Load("", "")
	if !errors.Is(err, ErrMissing) {
		t.Errorf("err = %v, want errors.Is(err, ErrMissing)", err)
	}
}

func TestLoadMalformedHex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := os.WriteFile(path, []byte("not-hex-at-all!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("", path); err == nil {
		t.Error("Load accepted malformed hex")
	}
}

func TestLoadWrongLengthKey(t *testing.T) {
	// Valid hex, but only 16 bytes instead of the required 32.
	path := filepath.Join(t.TempDir(), "master.key")
	short := strings.Repeat("ab", 16) + "\n"
	if err := os.WriteFile(path, []byte(short), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("", path); err == nil {
		t.Error("Load accepted a key of the wrong length")
	}
}

func TestLoadExplicitHexValue(t *testing.T) {
	// Generate a valid key file just to obtain valid hex, then load it via the
	// hexValue parameter instead of the filesystem.
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")
	if err := Generate(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hexValue := strings.TrimSpace(string(raw))

	// A bogus path proves the filesystem was never touched: if Load fell
	// through to reading it, this would fail with ErrMissing instead of
	// succeeding.
	m, err := Load(hexValue, filepath.Join(dir, "definitely-not-here.key"))
	if err != nil {
		t.Fatalf("Load with explicit hex value: %v", err)
	}
	aad := AAD("credential", "private_key", 1)
	ct, nonce, err := m.Seal([]byte("payload"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(ct, nonce, aad); err != nil {
		t.Errorf("Open on key loaded from hex value: %v", err)
	}
}

func TestSealOpenAADMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := Generate(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}

	ct, nonce, err := m.Seal([]byte("secret"), AAD("credential", "private_key", 1))
	if err != nil {
		t.Fatal(err)
	}
	// Same table and column, different row: the row/column binding must
	// refuse to open it.
	if _, err := m.Open(ct, nonce, AAD("credential", "private_key", 2)); err == nil {
		t.Error("Open succeeded with mismatched AAD (row binding did not hold)")
	}
}

func TestOpenTamperedCiphertext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := Generate(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	aad := AAD("credential", "private_key", 1)
	ct, nonce, err := m.Seal([]byte("secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte(nil), ct...)
	tampered[0] ^= 0xFF
	if _, err := m.Open(tampered, nonce, aad); err == nil {
		t.Error("Open succeeded on tampered ciphertext")
	}
}

func TestOpenWrongNonceLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")
	if err := Generate(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load("", path)
	if err != nil {
		t.Fatal(err)
	}
	aad := AAD("credential", "private_key", 1)
	ct, _, err := m.Seal([]byte("secret"), aad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(ct, []byte("too-short"), aad); err == nil {
		t.Error("Open succeeded with a wrong-length nonce")
	}
}
