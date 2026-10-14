package accounts

import (
	"testing"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

func TestPolicy(t *testing.T) {
	cases := []struct {
		pw   string
		code apperr.Code
	}{
		{"123456789", apperr.PasswordInvalid},                    // 9 characters
		{string(make([]rune, 129, 129)), apperr.PasswordInvalid}, // 129 characters
		{"1234567890", apperr.PasswordTooCommon},
		{"QWERTYUIOP", apperr.PasswordTooCommon}, // case-insensitive
		{"tide water", ""},                       // 10 characters with a space
		{"saltwater-hymn", ""},
	}
	for _, c := range cases {
		pw := c.pw
		if len([]rune(pw)) == 129 {
			pw = ""
			for i := 0; i < 129; i++ {
				pw += "a"
			}
		}
		err := CheckPolicy(pw)
		if c.code == "" {
			if err != nil {
				t.Errorf("%q: unexpected error %v", c.pw, err)
			}
			continue
		}
		e, ok := apperr.As(err)
		if !ok || e.Code != c.code {
			t.Errorf("%q: got %v, want %s", c.pw, err, c.code)
		}
	}
}

func TestHashRoundTripAndRehash(t *testing.T) {
	h, err := HashPassword("saltwater-hymn")
	if err != nil {
		t.Fatal(err)
	}
	if ok, rehash := VerifyPassword("saltwater-hymn", h); !ok || rehash {
		t.Fatalf("verify = %v, rehash = %v", ok, rehash)
	}
	if ok, _ := VerifyPassword("saltwater-hymm", h); ok {
		t.Fatal("wrong password accepted")
	}
	old, _ := hashWith("saltwater-hymn", argonParams{memoryKiB: 19 * 1024, time: 2, threads: 1, keyLen: 32, saltLen: 16})
	if ok, rehash := VerifyPassword("saltwater-hymn", old); !ok || !rehash {
		t.Fatalf("outdated params: verify = %v, rehash = %v", ok, rehash)
	}
}
