package stats

import (
	"math"
	"testing"
)

func near(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestWilsonKnownValues(t *testing.T) {
	cases := []struct {
		x, n   int64
		lo, hi float64
	}{
		{81, 263, 0.2553, 0.3662}, // Newcombe (1998), Statistics in Medicine 17, table I, method 3
		{61, 512, 0.0939, 0.1501}, // the example in docs/contracts/api.md (which rounds the bounds to 0.093, 0.150)
		{5, 20, 0.1119, 0.4687},
		{0, 10, 0, 0.2775},
		{10, 10, 0.7225, 1},
	}
	for _, c := range cases {
		lo, hi := Wilson(c.x, c.n, Z95)
		if !near(lo, c.lo, 5e-5) || !near(hi, c.hi, 5e-5) {
			t.Errorf("Wilson(%d/%d) = [%.4f, %.4f], want [%.4f, %.4f]", c.x, c.n, lo, hi, c.lo, c.hi)
		}
	}
}

func TestWilsonEmpty(t *testing.T) {
	if lo, hi := Wilson(0, 0, Z95); lo != 0 || hi != 1 {
		t.Fatalf("got [%v, %v]", lo, hi)
	}
}

func TestWilsonContainsRate(t *testing.T) {
	for n := int64(1); n < 60; n++ {
		for x := int64(0); x <= n; x++ {
			lo, hi := Wilson(x, n, Z95)
			p := float64(x) / float64(n)
			if lo > p+1e-12 || hi < p-1e-12 || lo < 0 || hi > 1 {
				t.Fatalf("%d/%d: [%v, %v] does not contain %v", x, n, lo, hi, p)
			}
		}
	}
}

func TestTwoProportionZKnownValues(t *testing.T) {
	cases := []struct {
		x1, n1, x2, n2 int64
		z, p           float64
	}{
		{20, 100, 30, 100, 1.63299, 0.10247},     // pooled p = 0.25, se = 0.061237
		{120, 1000, 150, 1000, 1.96305, 0.04964}, // just significant at 5 %
		{61, 512, 72, 500, 1.17025, 0.24190},
		{30, 100, 20, 100, -1.63299, 0.10247}, // symmetric: negative z, same p
		{45, 500, 45, 500, 0, 1},
	}
	for _, c := range cases {
		z, p, ok := TwoProportionZ(c.x1, c.n1, c.x2, c.n2)
		if !ok || !near(z, c.z, 5e-5) || !near(p, c.p, 5e-5) {
			t.Errorf("z-test %d/%d vs %d/%d = z %.5f p %.5f ok %v, want z %.5f p %.5f", c.x1, c.n1, c.x2, c.n2, z, p, ok, c.z, c.p)
		}
	}
}

func TestTwoProportionZDegenerate(t *testing.T) {
	if _, _, ok := TwoProportionZ(0, 0, 3, 10); ok {
		t.Error("empty control must not be testable")
	}
	if _, _, ok := TwoProportionZ(3, 10, 0, 0); ok {
		t.Error("empty variant must not be testable")
	}
	if z, p, ok := TwoProportionZ(0, 10, 0, 20); !ok || z != 0 || p != 1 {
		t.Errorf("both zero: z %v p %v ok %v", z, p, ok)
	}
	if z, p, ok := TwoProportionZ(10, 10, 20, 20); !ok || z != 0 || p != 1 {
		t.Errorf("both one: z %v p %v ok %v", z, p, ok)
	}
}
