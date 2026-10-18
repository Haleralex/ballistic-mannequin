package ids

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewCaseIDShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 10000; i++ {
		id, err := NewCaseID()
		if err != nil {
			t.Fatal(err)
		}
		if !IsCaseID(id) {
			t.Fatalf("bad id %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q after %d", id, i)
		}
		seen[id] = true
	}
}

func TestNewIDRejectsBiasedBytes(t *testing.T) {
	// 248..255 must be skipped; 0 → '0', 61 → 'z', 62 → '0', 247 → 247%62=61 → 'z'.
	src := bytes.NewReader([]byte{255, 248, 0, 61, 62, 247, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18})
	id, err := newID(src, 8)
	if err != nil {
		t.Fatal(err)
	}
	if want := "0z0z1234"; id != want {
		t.Fatalf("got %q want %q", id, want)
	}
}

func TestNewIDDistribution(t *testing.T) {
	counts := map[byte]int{}
	const n = 62 * 2000
	for i := 0; i < n/CaseIDLen; i++ {
		id, _ := NewCaseID()
		for j := 0; j < len(id); j++ {
			counts[id[j]]++
		}
	}
	if len(counts) != 62 {
		t.Fatalf("only %d distinct characters", len(counts))
	}
	for c, k := range counts {
		if k < 1400 || k > 2600 { // mean 2000, sd ≈ 44
			t.Errorf("character %q appears %d times", c, k)
		}
	}
}

func TestIsCaseID(t *testing.T) {
	for _, s := range []string{"a7F3kQ9x", "00000000", "zzzzzzzz"} {
		if !IsCaseID(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "a7F3kQ9", "a7F3kQ9xy", "a7F3kQ9-", "../../et", "a7F3kQ9é"} {
		if IsCaseID(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestNormalizeUUID(t *testing.T) {
	got, ok := NormalizeUUID("3F2504E0-4F89-41D3-9A0C-0305E82C3301")
	if !ok || got != "3f2504e0-4f89-41d3-9a0c-0305e82c3301" {
		t.Fatalf("got %q %v", got, ok)
	}
	for _, s := range []string{"", "3f2504e0-4f89-41d3-9a0c-0305e82c330", "3f2504e04f8941d39a0c0305e82c3301", "3f2504e0-4f89-41d3-9a0c-0305e82c330g", strings.Repeat("-", 36)} {
		if _, ok := NormalizeUUID(s); ok {
			t.Errorf("%q should be invalid", s)
		}
	}
}
