package core

import "hash/fnv"

// DefaultSeed is the item-memory seed used by NewTokenDictionary. Seed 0 is bit-compatible
// with the arena encoder's original hash stream, so the arena's committed reference results
// remain valid under the default seed.
const DefaultSeed uint64 = 0

// SeededHV deterministically derives the base hypervector for a token from (seed, token):
// the FNV-1a hash of the token, XORed with the seed, keys a splitmix64 stream that fills the
// 157 words. The same (seed, token) pair yields a bit-identical vector on every run, machine,
// and architecture — this is the item memory the project's determinism claim rests on
// (proven in CI by the committed golden vectors in core/testdata). Distinct tokens, or the
// same token under distinct seeds, yield quasi-orthogonal vectors (d_H ≈ Dimension/2).
func SeededHV(seed uint64, token string) Hypervector {
	h := fnv.New64a()
	h.Write([]byte(token))
	x := h.Sum64() ^ seed

	var hv Hypervector
	for i := 0; i < NumWords; i++ {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z = z ^ (z >> 31)
		hv.Vector[i] = z
	}
	hv.Vector[NumWords-1] &= LastWordMask
	return hv
}
