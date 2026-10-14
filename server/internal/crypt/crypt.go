// Package crypt holds token generation, hashing and the AES-GCM sealing used for
// transient secrets at rest (emailed link material, §5.1).
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// NewToken returns a random URL-safe token and the SHA-256 hash that is stored.
func NewToken() (token string, hash []byte) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// MAC returns a base64url HMAC-SHA256 of msg (used for CSRF tokens).
func MAC(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func EqualMAC(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }

type Sealer struct{ aead cipher.AEAD }

func NewSealer(key string) (*Sealer, error) {
	k := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plain string) string {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(s.aead.Seal(nonce, nonce, []byte(plain), nil))
}

func (s *Sealer) Open(sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	n := s.aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("crypt: sealed value too short")
	}
	plain, err := s.aead.Open(nil, raw[:n], raw[n:], nil)
	return string(plain), err
}
