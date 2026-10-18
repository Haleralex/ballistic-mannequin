// Package assign picks experiment variants deterministically from the visitor id and the experiment key.
package assign

import (
	"crypto/sha256"
	"encoding/binary"
)

// Bucket maps visitor+key to a number in [0, total). SHA-256 of the concatenation visitor_id + key, first 8 bytes
// big-endian, modulo total. The visitor id is a fixed-length canonical UUID, so the concatenation is unambiguous.
func Bucket(visitorID, key string, total uint64) uint64 {
	sum := sha256.Sum256([]byte(visitorID + key))
	return binary.BigEndian.Uint64(sum[:8]) % total
}

// Pick returns the variant for a visitor, weighted by weights (same length as variants). Negative weights count as
// zero. When all weights are zero, or the lengths differ, the first variant is returned. Returns "" for no variants.
func Pick(visitorID, key string, variants []string, weights []int) string {
	if len(variants) == 0 {
		return ""
	}
	if len(weights) != len(variants) {
		return variants[0]
	}
	var total uint64
	for _, w := range weights {
		if w > 0 {
			total += uint64(w)
		}
	}
	if total == 0 {
		return variants[0]
	}
	b := Bucket(visitorID, key, total)
	var acc uint64
	for i, w := range weights {
		if w <= 0 {
			continue
		}
		acc += uint64(w)
		if b < acc {
			return variants[i]
		}
	}
	return variants[len(variants)-1] // unreachable
}
