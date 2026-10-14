// Package fields normalizes and validates user-entered names (F-20).
package fields

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

const (
	MaxSongName     = 200
	MaxArtistName   = 200
	MaxPlaylistName = 100
	MaxGroupName    = 100
)

// Clean trims, NFC-normalizes and strips control characters.
func Clean(s string) string {
	s = norm.NFC.String(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// Name cleans s and checks it is non-empty and at most max characters.
func Name(field, s string, max int) (string, error) {
	c := Clean(s)
	if c == "" {
		return "", apperr.Field(field, "empty")
	}
	if utf8.RuneCountInString(c) > max {
		return "", apperr.WithDetails(apperr.InvalidField, "Invalid field: "+field,
			map[string]any{"field": field, "reason": "too_long", "max": max})
	}
	return c, nil
}

// NormalizeArtist is the identity key for artists: "the same normalized name"
// in R-ORG-2. It folds case and spacing only; fuzzy similarity is a client-side
// suggestion and never merges artists on its own.
func NormalizeArtist(name string) string {
	s := strings.ToLower(norm.NFKC.String(Clean(name)))
	return strings.Join(strings.Fields(s), " ")
}

// NormalizeEmail is the account identity key (R-AUTH-7).
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(norm.NFC.String(email)))
}

// ValidEmail is a deliberately loose syntax check; the address is never verified (R-AUTH-1).
func ValidEmail(email string) bool {
	if len(email) < 3 || len(email) > 254 || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	return at > 0 && at < len(email)-1 && strings.Contains(email[at+1:], ".")
}
