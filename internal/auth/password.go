package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	MinPasswordLength = 8
	// bcrypt ignores input past 72 bytes, so longer passwords would silently be truncated.
	MaxPasswordBytes = 72
)

var ErrPasswordPolicy = errors.New("password must be 8 to 72 bytes long")

func CheckPasswordPolicy(password string) error {
	if len(password) < MinPasswordLength || len(password) > MaxPasswordBytes {
		return ErrPasswordPolicy
	}
	return nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

// dummyHash is compared against when an account has no password, so a login attempt takes the same
// time whether or not the email is registered.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("biletflow-timing-equalizer"), bcrypt.DefaultCost)

// PasswordMatches reports whether password matches hash. A nil hash (code-only account) never matches.
func PasswordMatches(hash *string, password string) bool {
	if hash == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(*hash), []byte(password)) == nil
}

// NewSessionToken returns a random bearer token and the hash stored in place of it.
func NewSessionToken() (token, hash string) {
	raw := make([]byte, 32)
	_, _ = rand.Read(raw) // crypto/rand.Read never returns an error
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, TokenHash(token)
}

// TokenHash is the stored form of a session token; the token itself is never persisted.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
