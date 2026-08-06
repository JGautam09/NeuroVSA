package core

import (
	"math"
	"testing"
)

// quasiOrthogonalBand is the acceptance band for distances that should sit at the
// Dimension/2 noise floor: ±5·√Dimension, i.e. ±10σ of a fair-coin bit count. At the
// default 10,000 bits this is exactly the historical [4500, 5500] band; it scales with
// the hd_d* study dimensions instead of hardcoding the default.
func quasiOrthogonalBand() (lo, hi int) {
	slack := 5 * int(math.Sqrt(float64(Dimension)))
	return Dimension/2 - slack, Dimension/2 + slack
}

func TestGenerateRandom(t *testing.T) {
	hv := GenerateRandom()

	// Ensure the final word's excess bits (if any) are masked to 0
	if (hv.Vector[NumWords-1] & ^LastWordMask) != 0 {
		t.Errorf("GenerateRandom() failed to mask excess bits in the final word. Value: 0x%X", hv.Vector[NumWords-1])
	}

	// Verify that random hypervectors have roughly ~Dimension/2 active bits (near quasi-orthogonality)
	lo, hi := quasiOrthogonalBand()
	dist := HammingDistance(hv, ZeroHV())
	if dist < lo || dist > hi {
		t.Errorf("Random hypervector bit density outside expected range [%d, %d]: %d", lo, hi, dist)
	}
}

func TestBindSelfInverse(t *testing.T) {
	a := GenerateRandom()
	b := GenerateRandom()

	// A ⊗ B
	bound := a.Bind(b)

	// Unbind: (A ⊗ B) ⊗ B = A
	unbound := bound.Bind(b)

	dist := HammingDistance(a, unbound)
	if dist != 0 {
		t.Errorf("Bind unbinding failed: expected distance 0, got %d", dist)
	}
}

// Guardrail #3 Verification Test: Bind(A, Bind(A, B)) == B with 0 bit errors.
func TestBindExactEqualityGuardrail(t *testing.T) {
	for i := 0; i < 100; i++ {
		a := GenerateRandom()
		b := GenerateRandom()

		// Bind(A, Bind(A, B)) == B
		result := a.Bind(a.Bind(b))

		if result != b {
			t.Fatalf("Guardrail #3 Violation: Bind(A, Bind(A, B)) != B! Hamming distance: %d", HammingDistance(result, b))
		}
	}
}

// Guardrail #2 Verification Test: Even-vector Majority Vote bundling tie resolution.
func TestEvenVectorMajorityVoteTieBreaking(t *testing.T) {
	v1 := GenerateRandom()
	v2 := GenerateRandom()

	// Bundle 2 vectors (even N = 2)
	bundledEven := Bundle([]Hypervector{v1, v2})

	// Ensure word 156 bitwise masking is preserved
	if (bundledEven.Vector[NumWords-1] & ^LastWordMask) != 0 {
		t.Errorf("Guardrail #1 Violation in Bundle(): Word 156 contains unmasked ghost bits: 0x%X", bundledEven.Vector[NumWords-1])
	}
}

func TestPermuteAndInverse(t *testing.T) {
	hv := GenerateRandom()

	// Shift left by 5
	permuted := hv.Permute(5)

	// Shift right by 5 (inverse)
	restored := permuted.PermuteInv(5)

	dist := HammingDistance(hv, restored)
	if dist != 0 {
		t.Errorf("Permute/PermuteInv cycle failed: expected distance 0, got %d", dist)
	}
}

func TestBundlePreservesSimilarity(t *testing.T) {
	a := GenerateRandom()
	b := GenerateRandom()
	c := GenerateRandom()

	// Bundle A, B, C
	bundled := Bundle([]Hypervector{a, b, c})

	// Bundled vector should be closer to A, B, C (d_H < 5000) than to a random vector (~5000)
	dA := HammingDistance(bundled, a)
	dB := HammingDistance(bundled, b)
	dC := HammingDistance(bundled, c)

	if dA >= 4000 || dB >= 4000 || dC >= 4000 {
		t.Errorf("Bundled vector similarity expected < 4000 distance. Got dA=%d, dB=%d, dC=%d", dA, dB, dC)
	}
}

func TestTokenDictionary(t *testing.T) {
	dict := NewTokenDictionary()

	hv1 := dict.GetOrRegister("func_main")
	hv2 := dict.GetOrRegister("param_x")

	if dict.Size() != 2 {
		t.Errorf("Expected dictionary size 2, got %d", dict.Size())
	}

	// Exact lookup test
	token, dist := dict.LookupToken(hv1)
	if token != "func_main" || dist != 0 {
		t.Errorf("LookupToken exact match failed: expected ('func_main', 0), got ('%s', %d)", token, dist)
	}

	// Noisy query test (flip 50 bits out of 10,000)
	noisyHV := hv2
	for i := 0; i < 50; i++ {
		if noisyHV.GetBit(i) == 1 {
			noisyHV.ClearBit(i)
		} else {
			noisyHV.SetBit(i)
		}
	}

	tokenNoisy, distNoisy := dict.LookupToken(noisyHV)
	if tokenNoisy != "param_x" || distNoisy != 50 {
		t.Errorf("LookupToken noisy match failed: expected ('param_x', 50), got ('%s', %d)", tokenNoisy, distNoisy)
	}
}
