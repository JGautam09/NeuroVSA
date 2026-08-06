package core

import (
	"fmt"
	"math/bits"
	"testing"
)

// bundleReference is the naive per-position majority — the definition Bundle must match
// bit-for-bit, kept verbatim from the pre-bit-sliced implementation. World hashes, golden
// vectors, and committed arena results all depend on Bundle's exact output (including the
// tie-break), so the sliced version is not allowed to differ on any input.
func bundleReference(vectors []Hypervector) Hypervector {
	if len(vectors) == 0 {
		return Hypervector{}
	}
	if len(vectors) == 1 {
		return vectors[0]
	}

	n := len(vectors)
	var counts [Dimension]uint32
	for v := range vectors {
		for i := 0; i < NumWords; i++ {
			w := vectors[v].Vector[i]
			base := i * 64
			for w != 0 {
				counts[base+bits.TrailingZeros64(w)]++
				w &= w - 1 // clear lowest set bit
			}
		}
	}

	var res Hypervector
	for k := 0; k < Dimension; k++ {
		c2 := int(counts[k]) * 2
		if c2 > n {
			res.SetBit(k)
		} else if c2 == n && tieBreak.GetBit(k) == 1 {
			res.SetBit(k)
		}
	}

	res.Vector[NumWords-1] &= LastWordMask
	return res
}

// TestBundleMatchesReference: bit-identity across N = 0..64 on deterministic seeded
// inputs. Even N exercises the tie-break lanes (random pairs tie on ~50% of positions).
func TestBundleMatchesReference(t *testing.T) {
	for n := 0; n <= 64; n++ {
		vs := make([]Hypervector, n)
		for i := range vs {
			vs[i] = SeededHV(0xB17E, fmt.Sprintf("n%d-v%d", n, i))
		}
		got, want := Bundle(vs), bundleReference(vs)
		if got != want {
			t.Fatalf("Bundle diverges from the naive definition at N=%d", n)
		}
	}
}

// TestBundleTieLanes pins the tie-break explicitly: bundling a vector with its canonical
// complement ties on EVERY lane, so the result must be exactly the tieBreak vector
// (masked). This is the case a comparator bug would corrupt first.
func TestBundleTieLanes(t *testing.T) {
	a := SeededHV(0x71E, "tie-a")
	var b Hypervector
	for i := 0; i < NumWords; i++ {
		b.Vector[i] = ^a.Vector[i]
	}
	b.Vector[NumWords-1] &= LastWordMask

	got := Bundle([]Hypervector{a, b})
	want := bundleReference([]Hypervector{a, b})
	if got != want {
		t.Fatal("all-tie bundle diverges from reference")
	}
	for i := 0; i < NumWords; i++ {
		mask := ^uint64(0)
		if i == NumWords-1 {
			mask = LastWordMask
		}
		if got.Vector[i] != tieBreak.Vector[i]&mask {
			t.Fatalf("word %d: all-tie result must equal the tie-break vector", i)
		}
	}
}

// TestBundleUnanimityAndDominance: degenerate inputs with known closed-form answers.
func TestBundleUnanimityAndDominance(t *testing.T) {
	v := SeededHV(0xD0, "dom-v")
	for _, n := range []int{2, 3, 8, 17} {
		vs := make([]Hypervector, n)
		for i := range vs {
			vs[i] = v
		}
		if Bundle(vs) != v {
			t.Fatalf("unanimous bundle of %d copies must be the vector itself", n)
		}
	}

	// v appears 3 times against 2 distinct others: strict majority everywhere v has a bit,
	// and everywhere v lacks one the others can muster at most 2 of 5 votes plus v's 0 —
	// so the result is exactly v.
	other1, other2 := SeededHV(0xD0, "dom-o1"), SeededHV(0xD0, "dom-o2")
	if got := Bundle([]Hypervector{v, v, v, other1, other2}); got != bundleReference([]Hypervector{v, v, v, other1, other2}) {
		t.Fatalf("dominant-vector bundle diverges from reference: %v", got)
	}
}

// FuzzBundleDifferential: the standing guard — any (count, seed) pair must produce
// bit-identical sliced and reference outputs.
func FuzzBundleDifferential(f *testing.F) {
	f.Add(uint8(2), uint64(1))
	f.Add(uint8(3), uint64(2))
	f.Add(uint8(8), uint64(3))
	f.Add(uint8(33), uint64(4))
	f.Fuzz(func(t *testing.T, n uint8, seed uint64) {
		count := int(n%64) + 1
		vs := make([]Hypervector, count)
		for i := range vs {
			vs[i] = SeededHV(seed, fmt.Sprintf("fz%d", i))
		}
		if Bundle(vs) != bundleReference(vs) {
			t.Fatalf("divergence at count=%d seed=%d", count, seed)
		}
	})
}
