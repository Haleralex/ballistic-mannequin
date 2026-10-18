package assign

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

func vid(i int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", i, i*7919) }

func TestPickDeterministic(t *testing.T) {
	v, w := []string{"a", "b", "c"}, []int{1, 1, 1}
	for i := 0; i < 200; i++ {
		first := Pick(vid(i), "t1_positioning", v, w)
		for j := 0; j < 5; j++ {
			if got := Pick(vid(i), "t1_positioning", v, w); got != first {
				t.Fatalf("visitor %d: %s then %s", i, first, got)
			}
		}
	}
}

func TestPickKnownValue(t *testing.T) {
	// Pin the hash so a refactor cannot silently reshuffle every visitor.
	id := "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	sum := sha256.Sum256([]byte(id + "t1_positioning"))
	want := []string{"a", "b", "c"}[binary.BigEndian.Uint64(sum[:8])%3]
	if got := Pick(id, "t1_positioning", []string{"a", "b", "c"}, []int{1, 1, 1}); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got := Bucket(id, "t1_positioning", 1000); got != binary.BigEndian.Uint64(sum[:8])%1000 {
		t.Fatalf("bucket %d", got)
	}
}

func TestPickDependsOnKey(t *testing.T) {
	same := 0
	for i := 0; i < 1000; i++ {
		if Pick(vid(i), "t1", []string{"a", "b"}, []int{1, 1}) == Pick(vid(i), "t2", []string{"a", "b"}, []int{1, 1}) {
			same++
		}
	}
	if same < 400 || same > 600 { // independent experiments: about half agree
		t.Fatalf("%d of 1000 agree between experiments", same)
	}
}

func TestPickWeights(t *testing.T) {
	cases := []struct {
		variants []string
		weights  []int
	}{
		{[]string{"a", "b", "c"}, []int{1, 1, 1}},
		{[]string{"a", "b"}, []int{1, 3}},
		{[]string{"a", "b", "c"}, []int{5, 0, 5}},
		{[]string{"a", "b"}, []int{90, 10}},
	}
	const n = 30000
	for _, c := range cases {
		total := 0
		for _, w := range c.weights {
			total += w
		}
		counts := map[string]int{}
		for i := 0; i < n; i++ {
			counts[Pick(vid(i), "exp", c.variants, c.weights)]++
		}
		for i, v := range c.variants {
			p := float64(c.weights[i]) / float64(total)
			want := p * n
			sd := math.Sqrt(n * p * (1 - p))
			if got := float64(counts[v]); math.Abs(got-want) > 5*sd+0.5 {
				t.Errorf("weights %v: variant %s got %v want %.0f±%.0f", c.weights, v, got, want, 5*sd)
			}
		}
	}
}

func TestPickEdgeCases(t *testing.T) {
	if got := Pick(vid(1), "k", nil, nil); got != "" {
		t.Errorf("no variants: %q", got)
	}
	if got := Pick(vid(1), "k", []string{"a", "b"}, []int{0, 0}); got != "a" {
		t.Errorf("zero weights: %q", got)
	}
	if got := Pick(vid(1), "k", []string{"a", "b"}, []int{1}); got != "a" {
		t.Errorf("length mismatch: %q", got)
	}
	for i := 0; i < 100; i++ {
		if got := Pick(vid(i), "k", []string{"a", "b"}, []int{-3, 2}); got != "b" {
			t.Fatalf("negative weight: %q", got)
		}
	}
}
