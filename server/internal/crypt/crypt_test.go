package crypt

import (
	"bytes"
	"testing"
)

func TestTokens(t *testing.T) {
	a, ha := NewToken()
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Fatal("tokens must be random and long")
	}
	if !bytes.Equal(ha, HashToken(a)) {
		t.Fatal("stored hash must match")
	}
}

func TestSealer(t *testing.T) {
	s, _ := NewSealer("key-one")
	sealed := s.Seal("https://example.com/reset#token=abc")
	if got, err := s.Open(sealed); err != nil || got != "https://example.com/reset#token=abc" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if s.Seal("x") == s.Seal("x") {
		t.Fatal("nonces must differ")
	}
	other, _ := NewSealer("key-two")
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("opened with the wrong key")
	}
	if _, err := s.Open("not-base64!"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestMAC(t *testing.T) {
	if !EqualMAC(MAC("s", "m"), MAC("s", "m")) || EqualMAC(MAC("s", "m"), MAC("s", "n")) || EqualMAC(MAC("s", "m"), MAC("t", "m")) {
		t.Fatal("MAC")
	}
}
