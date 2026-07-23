package core

import (
	"crypto/rand"
	"fmt"
	"math/big"
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
func (hv Hypervector) Permute(shift int) Hypervector {
	shift = ((shift % Dimension) + Dimension) % Dimension
	if shift == 0 {
		return hv
	}

	var res Hypervector
	for m := 0; m < Dimension; m++ {
		// Output bit m gets input bit (m - shift + 10000) % 10000
		srcIdx := (m - shift + Dimension) % Dimension
		if hv.GetBit(srcIdx) == 1 {
			res.SetBit(m)
		}
	}
	res.Vector[NumWords-1] &= LastWordMask
	return res
}

// PermuteInv performs an inverse cyclic left shift (cyclic right shift) by 'shift' bits (ρ⁻¹).
func (hv Hypervector) PermuteInv(shift int) Hypervector {
	return hv.Permute(-shift)
}

// Bundle performs a bitwise Majority Vote (⊕) across N hypervectors.
// For each bit position, if the count of 1s > N/2, the result bit is 1.
// If count < N/2, result bit is 0.
// For ties (even N, count == N/2), deterministic tie-breaking is used.
func Bundle(vectors []Hypervector) Hypervector {
	if len(vectors) == 0 {
		return Hypervector{}
	}
	if len(vectors) == 1 {
		return vectors[0]
	}

	n := len(vectors)
	threshold := n / 2
	isEven := (n % 2 == 0)

	var res Hypervector
	for k := 0; k < Dimension; k++ {
		count := 0
		for vIdx := 0; vIdx < n; vIdx++ {
			if vectors[vIdx].GetBit(k) == 1 {
				count++
			}
		}

		if count > threshold {
			res.SetBit(k)
		} else if isEven && count == threshold {
			// Deterministic tie-breaking using pseudo-random bit derived from bit index k
			if (k*37+13)%2 == 1 {
				res.SetBit(k)
			}
		}
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
