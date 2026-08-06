package core

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"math/bits"
)

const (
	// Dimension is the total number of bits in a hypervector.
	Dimension = 10000
	// NumWords is the number of uint64 words required to store 10,000 bits.
	NumWords = 157
	// LastWordMask keeps only the lower 16 bits (bits 0-15) of word 156 (156 * 64 + 16 = 10000).
	LastWordMask uint64 = 0xFFFF
)

// Hypervector represents a 10,000-bit dense binary vector packed into 157 uint64 words.
type Hypervector struct {
	Vector [NumWords]uint64
}

// ValidateCanonical reports an error if hv is not a canonical Dimension-bit vector — that
// is, if any bit above position Dimension-1 (the unused high 48 bits of the final word) is
// set. Every vector arriving from an untrusted, external encoding (hex, JSON, binary image,
// network) MUST pass this before use: a set excess bit indexes the per-bit vote-counter
// array (`[Dimension]uint32`) out of range in the associative memory, panicking the
// process (in wasm, the whole engine). We REJECT rather than silently mask, because masking
// would alter bytes an ed25519 signature covers — producing verify/no-verify inconsistency
// between a pack's declared and effective content.
func (hv Hypervector) ValidateCanonical() error {
	if hv.Vector[NumWords-1]&^LastWordMask != 0 {
		return fmt.Errorf("non-canonical hypervector: %d excess bit(s) set in the final word (only bits 0-15 are valid)",
			bits.OnesCount64(hv.Vector[NumWords-1]&^LastWordMask))
	}
	return nil
}

// GenerateRandom creates a hypervector with uniformly distributed random bits.
// Excess bits in word 156 (bits 16-63) are strictly masked to zero.
func GenerateRandom() Hypervector {
	var hv Hypervector
	buf := make([]byte, NumWords*8)
	_, err := rand.Read(buf)
	if err != nil {
		panic(fmt.Sprintf("failed to generate random bytes: %v", err))
	}

	for i := 0; i < NumWords; i++ {
		hv.Vector[i] = uint64(buf[i*8]) |
			(uint64(buf[i*8+1]) << 8) |
			(uint64(buf[i*8+2]) << 16) |
			(uint64(buf[i*8+3]) << 24) |
			(uint64(buf[i*8+4]) << 32) |
			(uint64(buf[i*8+5]) << 40) |
			(uint64(buf[i*8+6]) << 48) |
			(uint64(buf[i*8+7]) << 56)
	}

	// Mask out excess bits in the last word
	hv.Vector[NumWords-1] &= LastWordMask
	return hv
}

// GetBit returns the bit value (0 or 1) at global index k (0 <= k < 10000).
func (hv *Hypervector) GetBit(k int) uint64 {
	word := k / 64
	offset := k % 64
	return (hv.Vector[word] >> offset) & 1
}

// SetBit sets the bit value at global index k (0 <= k < 10000) to 1.
func (hv *Hypervector) SetBit(k int) {
	word := k / 64
	offset := k % 64
	hv.Vector[word] |= (1 << offset)
}

// ClearBit sets the bit value at global index k (0 <= k < 10000) to 0.
func (hv *Hypervector) ClearBit(k int) {
	word := k / 64
	offset := k % 64
	hv.Vector[word] &= ^(1 << offset)
}

// Bind performs bitwise XOR (⊗) between two hypervectors.
// XOR is self-inverse: A ⊗ B ⊗ B = A.
func (hv Hypervector) Bind(other Hypervector) Hypervector {
	var res Hypervector
	for i := 0; i < NumWords; i++ {
		res.Vector[i] = hv.Vector[i] ^ other.Vector[i]
	}
	res.Vector[NumWords-1] &= LastWordMask
	return res
}

// Permute performs a cyclic left shift by 'shift' bits across the 10,000-bit boundary (ρ).
// Bit k moves to position (k + shift) % 10000.
//
// Implemented as a word-level rotate over the packed [NumWords]uint64 array, so it costs
// O(NumWords) word operations rather than O(Dimension) bit operations. Because Dimension
// (10,000) is not a multiple of 64 (= 156*64 + 16), a D-bit cyclic left rotate by s is
// composed from two linear multiword shifts over the full 157-word (10,048-slot) array:
//
//	Permute(s) = mask( shiftLeftWords(X, s) | shiftRightWords(X, D-s) )
//
// The left shift carries the non-wrapping bits (k -> k+s where k+s < D); the right shift
// carries the bits that wrap past bit D-1 (k -> k+s-D); LastWordMask then discards the
// unused high slots 10000..10047. The two terms occupy disjoint bit ranges, so OR merges
// them exactly.
func (hv Hypervector) Permute(shift int) Hypervector {
	s := ((shift % Dimension) + Dimension) % Dimension
	if s == 0 {
		return hv
	}

	var left, right Hypervector
	shiftLeftWords(&left.Vector, &hv.Vector, s)
	shiftRightWords(&right.Vector, &hv.Vector, Dimension-s)

	var res Hypervector
	for i := 0; i < NumWords; i++ {
		res.Vector[i] = left.Vector[i] | right.Vector[i]
	}
	res.Vector[NumWords-1] &= LastWordMask
	return res
}

// shiftLeftWords sets dst = src << n over the packed word array, treating the array as one
// big little-endian bit string (slot j = word j/64, bit j%64). Bits shifted past the top of
// the array are dropped. Relies on Go's defined shift semantics (x >> 64 == 0).
func shiftLeftWords(dst, src *[NumWords]uint64, n int) {
	wordShift := n / 64
	bitShift := uint(n % 64)
	for i := NumWords - 1; i >= 0; i-- {
		var v uint64
		if s := i - wordShift; s >= 0 {
			v = src[s] << bitShift
			if bitShift != 0 && s >= 1 {
				v |= src[s-1] >> (64 - bitShift)
			}
		}
		dst[i] = v
	}
}

// shiftRightWords sets dst = src >> n over the packed word array (see shiftLeftWords).
// Bits shifted below slot 0 are dropped.
func shiftRightWords(dst, src *[NumWords]uint64, n int) {
	wordShift := n / 64
	bitShift := uint(n % 64)
	for i := 0; i < NumWords; i++ {
		var v uint64
		if s := i + wordShift; s < NumWords {
			v = src[s] >> bitShift
			if bitShift != 0 && s+1 < NumWords {
				v |= src[s+1] << (64 - bitShift)
			}
		}
		dst[i] = v
	}
}

// PermuteInv performs an inverse cyclic left shift (cyclic right shift) by 'shift' bits (ρ⁻¹).
func (hv Hypervector) PermuteInv(shift int) Hypervector {
	return hv.Permute(-shift)
}

// tieBreak is a fixed, high-entropy vector used to resolve exact majority ties in Bundle
// when N is even. Using a fixed pseudo-random pattern (rather than a k-parity rule such as
// the old (k*37+13)%2, which collapses to "k is even") avoids injecting a structured stripe
// into every bundle, keeps the result symmetric across the bundled vectors, and stays
// deterministic across program runs — important for reproducible encodings and persistence.
// Bits are generated by a seeded splitmix64 over the word index.
var tieBreak = func() Hypervector {
	var hv Hypervector
	x := uint64(0x9E3779B97F4A7C15)
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
}()

// TieBreakBit returns the deterministic tie-break bit for position k, matching the rule
// Bundle uses to resolve exact majority ties. It is exposed so other majority-vote consumers
// (notably the associative-memory counter vector) resolve ties identically to Bundle, which
// keeps the resulting vector at ~50% density instead of biasing it toward zero.
func TieBreakBit(k int) bool {
	return tieBreak.GetBit(k) == 1
}

// Bundle performs a bitwise Majority Vote (⊕) across N hypervectors.
// For each bit position, if the count of 1s > N/2, the result bit is 1.
// If count < N/2, result bit is 0.
// For ties (even N, count == N/2), deterministic tie-breaking is used (see tieBreak).
//
// Implementation: BIT-SLICED counting — 64 bit positions are counted simultaneously per
// word column. For each of the NumWords columns, the N input words are summed into a
// bit-sliced counter (planes[p] holds bit p of every lane's count) by ripple-carry
// addition, then a bit-sliced comparator computes count > N/2 (and count == N/2 for the
// even-N tie lanes) across all 64 lanes at once. Total work is O(NumWords · N · log N)
// word operations, with no [Dimension]-sized tally and no per-bit loop. The output is
// bit-identical to the naive per-position definition, including the tie-break —
// enforced by a differential test and fuzz target against the reference implementation.
func Bundle(vectors []Hypervector) Hypervector {
	if len(vectors) == 0 {
		return Hypervector{}
	}
	if len(vectors) == 1 {
		return vectors[0]
	}

	n := len(vectors)
	threshold := uint64(n / 2)
	// Lane counts never exceed n, so bits.Len64(n) planes can be non-zero; the ripple
	// carry cannot escape plane nb-1. planes[64] would only be reachable for n ≥ 2^64.
	nb := bits.Len64(uint64(n))
	even := n%2 == 0

	var res Hypervector
	var planes [64]uint64
	for w := 0; w < NumWords; w++ {
		for p := 0; p < nb; p++ {
			planes[p] = 0
		}
		// Sum the column: add each input word into the bit-sliced counter (ripple carry).
		for v := range vectors {
			carry := vectors[v].Vector[w]
			for p := 0; carry != 0; p++ {
				t := planes[p] & carry
				planes[p] ^= carry
				carry = t
			}
		}
		// Compare every lane's count against the constant threshold, MSB down:
		// gt lanes have count > threshold; eq lanes (tracked for even N) have count == it.
		var gt uint64
		eq := ^uint64(0)
		for p := nb - 1; p >= 0; p-- {
			var tb uint64
			if threshold>>uint(p)&1 == 1 {
				tb = ^uint64(0)
			}
			gt |= eq & planes[p] &^ tb
			eq &= ^(planes[p] ^ tb)
		}
		word := gt
		if even {
			// Exact tie (only possible for even N): deterministic, symmetric tie-break.
			word |= eq & tieBreak.Vector[w]
		}
		res.Vector[w] = word
	}

	res.Vector[NumWords-1] &= LastWordMask
	return res
}

// Helper to generate a zero hypervector
func ZeroHV() Hypervector {
	return Hypervector{}
}

// ZeroHVCheck checks if hypervector is all zeros
func (hv Hypervector) IsZero() bool {
	for i := 0; i < NumWords; i++ {
		if hv.Vector[i] != 0 {
			return false
		}
	}
	return true
}

// CryptoRandInt returns a random integer up to max
func CryptoRandInt(max int64) int64 {
	nBig, err := rand.Int(rand.Reader, big.NewInt(max))
	if err != nil {
		return 0
	}
	return nBig.Int64()
}
