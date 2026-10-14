package fields

import (
	"strings"
	"testing"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

func TestName(t *testing.T) {
	got, err := Name("name", "  Har\u0000bor\tLights \n", 200)
	if err != nil || got != "HarborLights" {
		t.Fatalf("control characters and spacing: %q %v", got, err)
	}
	// NFC: a decomposed é counts as one character.
	if got, _ := Name("name", "Beyoncé", 7); got != "Beyoncé" {
		t.Fatalf("NFC: %q", got)
	}
	for _, in := range []string{"", "   ", "\u0000"} {
		if _, err := Name("name", in, 10); err == nil {
			t.Fatalf("empty %q accepted", in)
		}
	}
	if _, err := Name("name", strings.Repeat("é", 101), 100); err == nil {
		t.Fatal("101 characters accepted")
	} else if e, _ := apperr.As(err); e.Code != apperr.InvalidField || e.Details["reason"] != "too_long" {
		t.Fatalf("error: %v", err)
	}
	if _, err := Name("name", strings.Repeat("é", 100), 100); err != nil {
		t.Fatal("100 characters refused")
	}
}

func TestNormalizeArtist(t *testing.T) {
	if NormalizeArtist("  The   LOW tides ") != NormalizeArtist("the low tides") {
		t.Fatal("case and spacing must fold")
	}
	// Only case and spacing: similar names stay distinct artists (R-ORG-2).
	for _, pair := range [][2]string{{"Beyoncé", "Beyonce"}, {"AC/DC", "ACDC"}, {"The Beatles", "Beatles"}} {
		if NormalizeArtist(pair[0]) == NormalizeArtist(pair[1]) {
			t.Fatalf("%q and %q were merged", pair[0], pair[1])
		}
	}
}

func TestEmail(t *testing.T) {
	if NormalizeEmail("  Alex@Example.COM ") != "alex@example.com" {
		t.Fatal("normalize")
	}
	for _, ok := range []string{"a@b.co", "alex+music@example.com"} {
		if !ValidEmail(ok) {
			t.Fatalf("%q refused", ok)
		}
	}
	for _, bad := range []string{"", "alex", "@example.com", "alex@", "alex@example", "a b@example.com"} {
		if ValidEmail(bad) {
			t.Fatalf("%q accepted", bad)
		}
	}
}
