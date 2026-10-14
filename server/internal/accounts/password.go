package accounts

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"

	"github.com/henriquepaulalima/song-sleeve/server/internal/apperr"
)

// Starting argon2id parameters (R-AUTH-9). They are stored with each hash, so a
// change here rehashes on the next successful login.
type argonParams struct {
	memoryKiB uint32
	time      uint32
	threads   uint8
	keyLen    uint32
	saltLen   int
}

var currentParams = argonParams{memoryKiB: 64 * 1024, time: 3, threads: 1, keyLen: 32, saltLen: 16}

//go:embed common-passwords.txt
var commonPasswordsRaw string

var commonPasswords = func() map[string]struct{} {
	set := make(map[string]struct{}, 10000)
	sc := bufio.NewScanner(strings.NewReader(commonPasswordsRaw))
	for sc.Scan() {
		if w := strings.TrimSpace(sc.Text()); w != "" {
			set[strings.ToLower(w)] = struct{}{}
		}
	}
	return set
}()

// normalizePassword applies Unicode NFC before hashing (R-AUTH-9).
func normalizePassword(pw string) string { return norm.NFC.String(pw) }

// CheckPolicy enforces 10–128 characters with no composition rules and refuses the
// 10,000 most common passwords (D-31).
func CheckPolicy(pw string) error {
	pw = normalizePassword(pw)
	n := utf8.RuneCountInString(pw)
	if n < 10 || n > 128 {
		return apperr.WithDetails(apperr.PasswordInvalid, "Use 10 to 128 characters.", map[string]any{"min": 10, "max": 128})
	}
	if _, common := commonPasswords[strings.ToLower(pw)]; common {
		return apperr.New(apperr.PasswordTooCommon, "This password is too common.")
	}
	return nil
}

func HashPassword(pw string) (string, error) {
	return hashWith(normalizePassword(pw), currentParams)
}

func hashWith(pw string, p argonParams) (string, error) {
	salt := make([]byte, p.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(pw), salt, p.time, p.memoryKiB, p.threads, p.keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memoryKiB, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against an encoded hash and reports whether the hash
// uses outdated parameters and should be replaced.
func VerifyPassword(pw, encoded string) (ok bool, needsRehash bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, false
	}
	var p argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memoryKiB, &p.time, &p.threads); err != nil {
		return false, false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false, false
	}
	got := argon2.IDKey([]byte(normalizePassword(pw)), salt, p.time, p.memoryKiB, p.threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false
	}
	rehash := p.memoryKiB != currentParams.memoryKiB || p.time != currentParams.time ||
		p.threads != currentParams.threads || uint32(len(want)) != currentParams.keyLen
	return true, rehash
}

// dummyHash is verified when an email is unknown so login timing doesn't reveal
// which emails have accounts.
var dummyHash, _ = HashPassword("song-sleeve-dummy-password")
