package core

import (
	"testing"
)

func TestGenerateRandom(t *testing.T) {
	hv := GenerateRandom()

	// Ensure word 156 has bits 16-63 masked to 0
	if (hv.Vector[NumWords-1] & ^LastWordMask) != 0 {
		t.Errorf("GenerateRandom() failed to mask excess bits in word 156. Value: 0x%X", hv.Vector[NumWords-1])
	}

	// Verify that random hypervectors have roughly ~5,000 active bits (near quasi-orthogonality)
	dist := HammingDistance(hv, ZeroHV())
	if dist < 4500 || dist > 5500 {
		t.Errorf("Random hypervector bit density outside expected range [4500, 5500]: %d", dist)
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
