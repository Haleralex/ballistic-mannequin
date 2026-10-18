// Package ids makes the public identifiers of the service: short random case ids and checks for visitor ids.
package ids

import (
	"crypto/rand"
	"fmt"
	"io"
	"strings"
)

// Alphabet is the base62 alphabet of case ids.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// CaseIDLen is the length of a case id (8 base62 characters, about 47.6 bits).
const CaseIDLen = 8

// NewCaseID returns a random 8-character base62 id from crypto/rand.
func NewCaseID() (string, error) { return newID(rand.Reader, CaseIDLen) }

// newID draws bytes from r and keeps those below 248 (= 4 × 62) so every character is equally likely.
func newID(r io.Reader, n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, n*2)
	for len(out) < n {
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", fmt.Errorf("ids: read random: %w", err)
		}
		for _, b := range buf {
			if b >= 248 {
				continue
			}
			out = append(out, Alphabet[b%62])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}

// IsCaseID reports whether s has the shape of a case id.
func IsCaseID(s string) bool {
	if len(s) != CaseIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(Alphabet, s[i]) < 0 {
			return false
		}
	}
	return true
}

// NormalizeUUID checks that s is a UUID in the canonical 8-4-4-4-12 hex form and returns it lower-cased.
// Any version is accepted; browsers send v4 from crypto.randomUUID.
func NormalizeUUID(s string) (string, bool) {
	if len(s) != 36 {
		return "", false
	}
	b := []byte(strings.ToLower(s))
	for i, c := range b {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return "", false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return "", false
			}
		}
	}
	return string(b), true
}
