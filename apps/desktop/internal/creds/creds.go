// Package creds keeps the bearer token in the OS credential store (§7.12): the
// Secret Service on Linux (libsecret), Credential Manager on Windows. When no
// store is running (a bare window manager without a keyring daemon), it falls back
// to a file only this user can read, and says so.
package creds

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const service = "Song Sleeve"

type Store struct {
	fallbackDir string
	// Keyring is false when the OS store was unavailable and the file fallback is
	// in use; Settings shows a note.
	Keyring bool
}

// NewFile uses only the private file, never the OS store (tests, portable runs).
func NewFile(dir string) *Store { return &Store{fallbackDir: dir} }

func New(fallbackDir string) *Store {
	s := &Store{fallbackDir: fallbackDir, Keyring: true}
	if err := keyring.Set(service, "probe", "1"); err != nil {
		s.Keyring = false
	} else {
		_ = keyring.Delete(service, "probe")
	}
	return s
}

func (s *Store) file(accountID string) string {
	return filepath.Join(s.fallbackDir, "token-"+filepath.Base(accountID))
}

// Get returns the account's token, or "" if none is stored.
func (s *Store) Get(accountID string) (string, error) {
	if s.Keyring {
		t, err := keyring.Get(service, accountID)
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		return t, err
	}
	b, err := os.ReadFile(s.file(accountID))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return strings.TrimSpace(string(b)), err
}

func (s *Store) Set(accountID, token string) error {
	if s.Keyring {
		return keyring.Set(service, accountID, token)
	}
	if err := os.MkdirAll(s.fallbackDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.file(accountID), []byte(token), 0o600)
}

// Delete removes the token on logout; a missing token is fine.
func (s *Store) Delete(accountID string) error {
	if s.Keyring {
		if err := keyring.Delete(service, accountID); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return err
		}
		return nil
	}
	if err := os.Remove(s.file(accountID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
