// Package keys holds the Master Key and the credential envelope.
//
// Two rules that are not negotiable (ADR-0011):
//   - The server refuses to start if the Master Key is missing or malformed.
//     It never generates a replacement, because silently regenerating is how a
//     database becomes permanently undecryptable.
//   - Every ciphertext is bound to the column and row it belongs to via AES-GCM
//     additional data, so a ciphertext lifted from one row cannot be replayed
//     into another.
package keys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

const KeyLen = 32 // AES-256

var ErrMissing = errors.New("master key not found")

type Master struct {
	aead cipher.AEAD
	Path string // for the diagnostics page; contents are never shown
}

// Generate writes a new Master Key to path with 0600, refusing to overwrite.
func Generate(path string) error {
	key := make([]byte, KeyLen)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists; refusing to overwrite a master key", path)
		}
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		return err
	}
	return f.Chmod(0o600)
}

// Load reads the Master Key from an explicit hex value or a key file. Either a
// missing file or a malformed one is a startup failure, never a prompt to
// generate.
func Load(hexValue, path string) (*Master, error) {
	var raw string
	switch {
	case hexValue != "":
		raw, path = hexValue, "(NAGIPATH_MASTER_KEY)"
	case path != "":
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w at %s: run `nagipath keygen --out %s` once, then back that file up",
					ErrMissing, path, path)
			}
			return nil, err
		}
		raw = string(b)
	default:
		return nil, fmt.Errorf("%w: set --master-key-file or NAGIPATH_MASTER_KEY", ErrMissing)
	}

	key, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("master key at %s is malformed (expected %d hex characters): %w",
			path, KeyLen*2, err)
	}
	if len(key) != KeyLen {
		return nil, fmt.Errorf("master key at %s is %d bytes, want %d", path, len(key), KeyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Master{aead: aead, Path: path}, nil
}

// AAD binds a ciphertext to exactly one column of one row, e.g.
// "credential:private_key:17". Decryption of a value moved elsewhere fails.
func AAD(table, column string, rowID int64) []byte {
	return []byte(fmt.Sprintf("%s:%s:%d", table, column, rowID))
}

func (m *Master) Seal(plaintext, aad []byte) (ct, nonce []byte, err error) {
	nonce = make([]byte, m.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return m.aead.Seal(nil, nonce, plaintext, aad), nonce, nil
}

func (m *Master) Open(ct, nonce, aad []byte) ([]byte, error) {
	if len(nonce) != m.aead.NonceSize() {
		return nil, errors.New("stored nonce has the wrong length")
	}
	pt, err := m.aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: wrong master key, or the record was tampered with")
	}
	return pt, nil
}
