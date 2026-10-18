// Package stats has the statistics of the A/B dashboard: Wilson score intervals and the two-proportion z-test.
package stats

import "math"

// Z95 is the two-sided 95 % quantile of the standard normal distribution.
const Z95 = 1.959963984540054

// Wilson returns the Wilson score interval for x successes out of n at quantile z. For n = 0 it is [0, 1].
func Wilson(x, n int64, z float64) (lo, hi float64) {
	if n <= 0 {
		return 0, 1
	}
	nf := float64(n)
	p := float64(x) / nf
	z2 := z * z
	denom := 1 + z2/nf
	center := (p + z2/(2*nf)) / denom
	half := z * math.Sqrt(p*(1-p)/nf+z2/(4*nf*nf)) / denom
	lo, hi = center-half, center+half
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	return lo, hi
}

// TwoProportionZ compares x1/n1 (the control) with x2/n2 using the pooled two-proportion z-test.
// It returns z (positive when the second rate is higher) and the two-sided p-value.
// ok is false when either group is empty: no test is possible. When both rates are 0 or both 1 the
// pooled standard error is zero; then z = 0 and p = 1.
func TwoProportionZ(x1, n1, x2, n2 int64) (z, p float64, ok bool) {
	if n1 <= 0 || n2 <= 0 {
		return 0, math.NaN(), false
	}
	p1 := float64(x1) / float64(n1)
	p2 := float64(x2) / float64(n2)
	pool := float64(x1+x2) / float64(n1+n2)
	se := math.Sqrt(pool * (1 - pool) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		return 0, 1, true
	}
	z = (p2 - p1) / se
	return z, math.Erfc(math.Abs(z) / math.Sqrt2), true
}
