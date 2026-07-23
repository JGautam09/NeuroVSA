package core

import "testing"

// permuteNaive is the original O(Dimension) bit-by-bit reference implementation of Permute.
// The optimized word-level Permute must match it bit-for-bit for every shift, so this stays
// in the test suite as the correctness oracle for the fast path.
func permuteNaive(hv Hypervector, shift int) Hypervector {
	shift = ((shift % Dimension) + Dimension) % Dimension
	if shift == 0 {
		return hv
	}
	var res Hypervector
	for m := 0; m < Dimension; m++ {
		srcIdx := (m - shift + Dimension) % Dimension
		if hv.GetBit(srcIdx) == 1 {
			res.SetBit(m)
		}
	}
	res.Vector[NumWords-1] &= LastWordMask
	return res
}

func TestPermuteMatchesNaive(t *testing.T) {
	shifts := []int{
		0, 1, 2, 15, 16, 17, 31, 32, 33, 63, 64, 65, 127, 128, 129,
		156 * 64, 156*64 + 15, 9998, 9999, Dimension, Dimension + 1,
		-1, -16, -64, -9999, 12345, -12345,
	}
	for iter := 0; iter < 200; iter++ {
		hv := GenerateRandom()
		for _, s := range shifts {
			got := hv.Permute(s)
			want := permuteNaive(hv, s)
			if got != want {
				t.Fatalf("Permute(%d) mismatch (iter %d): hamming=%d", s, iter, HammingDistance(got, want))
			}
		}
	}
}

func TestPermuteRoundTripAllShifts(t *testing.T) {
	hv := GenerateRandom()
	for _, s := range []int{1, 7, 16, 64, 100, 1000, 5000, 9999} {
		if got := hv.Permute(s).PermuteInv(s); got != hv {
			t.Fatalf("Permute/PermuteInv(%d) round-trip failed: hamming=%d", s, HammingDistance(got, hv))
		}
	}
}

// A permuted random vector must remain quasi-orthogonal to the original (distance ~5000),
// confirming the rotate actually moves bits rather than returning a near-identical vector.
func TestPermuteDecorrelates(t *testing.T) {
	hv := GenerateRandom()
	for _, s := range []int{1, 3, 64, 500, 5000} {
		d := HammingDistance(hv, hv.Permute(s))
		if d < 4500 || d > 5500 {
			t.Errorf("Permute(%d) distance from original outside [4500,5500]: %d", s, d)
		}
	}
}

// bundleNaive is the O(N*Dimension) bit-by-bit reference for Bundle; the optimized
// word-tally Bundle must match it bit-for-bit.
func bundleNaive(vectors []Hypervector) Hypervector {
	if len(vectors) == 0 {
		return Hypervector{}
	}
	if len(vectors) == 1 {
		return vectors[0]
	}
	n := len(vectors)
	threshold := n / 2
	isEven := n%2 == 0
	var res Hypervector
	for k := 0; k < Dimension; k++ {
		count := 0
		for _, v := range vectors {
			if v.GetBit(k) == 1 {
				count++
			}
		}
		if count > threshold {
			res.SetBit(k)
		} else if isEven && count == threshold && tieBreak.GetBit(k) == 1 {
			res.SetBit(k)
		}
	}
	res.Vector[NumWords-1] &= LastWordMask
	return res
}

func TestBundleMatchesNaive(t *testing.T) {
	for _, n := range []int{2, 3, 4, 5, 8, 12, 16, 31} {
		for iter := 0; iter < 20; iter++ {
			vs := make([]Hypervector, n)
			for i := range vs {
				vs[i] = GenerateRandom()
			}
			if got, want := Bundle(vs), bundleNaive(vs); got != want {
				t.Fatalf("Bundle(n=%d) mismatch iter %d: hamming=%d", n, iter, HammingDistance(got, want))
			}
		}
	}
}

// The even-N tie-break must be deterministic across calls and symmetric — it must not
// collapse the bundle onto either input vector.
func TestBundleTieBreakDeterministicAndSymmetric(t *testing.T) {
	a := GenerateRandom()
	b := GenerateRandom()

	first := Bundle([]Hypervector{a, b})
	for i := 0; i < 20; i++ {
		if got := Bundle([]Hypervector{a, b}); got != first {
			t.Fatalf("Bundle tie-break not deterministic across calls (iter %d)", i)
		}
	}

	dA := HammingDistance(first, a)
	dB := HammingDistance(first, b)
	if dA < 500 || dB < 500 {
		t.Errorf("Bundle(a,b) collapsed onto an input: dA=%d dB=%d", dA, dB)
	}
}
