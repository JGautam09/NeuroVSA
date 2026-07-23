package engine

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"

	"github.com/JGautam09/NeuroVSA/core"
)

// On-disk format constants. The file is a fixed-size image:
//
//	[0:4]   magic "NVSA"
//	[4:6]   version (uint16, little-endian)
//	[6:8]   reserved / padding
//	[8:12]  dimension (uint32) — sanity check against the build
//	[12:16] numWords  (uint32) — sanity check against the build
//	[16:24] total     (uint64) — number of associations stored
//	[24 .. 24+countsSize)        counts[Dimension] (uint32 LE) — the per-bit vote tally
//	[.. +matrixSize)             matrix[NumWords]  (uint64 LE) — materialized majority vector
const (
	memMagic    = "NVSA"
	memVersion  = uint16(1)
	memHeader   = 24
	countsBytes = core.Dimension * 4
	matrixBytes = core.NumWords * 8
	memFileSize = memHeader + countsBytes + matrixBytes
)

// AssociativeMemory stores hypervector sequence associations as a per-bit vote tally.
//
// Instead of retaining every bound pair and re-bundling the whole history on each write
// (which was O(N·D) per store and O(N·D) memory), it keeps a fixed [Dimension]uint32 counter
// vector. Each StoreAssociation increments the counters for the set bits of the bound pair —
// O(D) work, independent of the number of prior associations N, and constant memory. The
// majority (memory) vector is materialized from the counters against the running total.
type AssociativeMemory struct {
	mu       sync.RWMutex
	counts   [core.Dimension]uint32
	total    uint64
	matrix   core.Hypervector // cached majority vector, kept in sync on every store
	readOnly bool             // true for OpenReadOnly instances (counters not loaded)
}

// NewAssociativeMemory initializes an empty associative memory instance.
func NewAssociativeMemory() *AssociativeMemory {
	return &AssociativeMemory{}
}

// StoreAssociation binds a context vector with a target token vector and folds it into the
// vote tally: for each set bit of (V_context ⊗ V_target), the corresponding counter is
// incremented. The majority vector is then re-materialized. O(D), independent of history.
func (am *AssociativeMemory) StoreAssociation(contextHV, targetHV core.Hypervector) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.readOnly {
		panic("StoreAssociation called on a read-only (OpenReadOnly) memory")
	}

	bound := contextHV.Bind(targetHV)
	for i := 0; i < core.NumWords; i++ {
		w := bound.Vector[i]
		base := i * 64
		for w != 0 {
			am.counts[base+bits.TrailingZeros64(w)]++
			w &= w - 1 // clear lowest set bit
		}
	}
	am.total++
	am.rematerializeLocked()
}

// rematerializeLocked recomputes the cached majority vector from the counters, so it is
// bit-for-bit identical to core.Bundle over the same set of bound pairs: a bit is set when
// strictly more than half voted for it, and exact ties (even total, counts*2 == total) use
// the same deterministic tie-break as Bundle. Resolving ties (rather than dropping them to
// zero) keeps the memory vector near 50% density, which is required for clean unbinding.
func (am *AssociativeMemory) rematerializeLocked() {
	var m core.Hypervector
	for k := 0; k < core.Dimension; k++ {
		c2 := uint64(am.counts[k]) * 2
		if c2 > am.total || (c2 == am.total && am.total > 0 && core.TieBreakBit(k)) {
			m.SetBit(k)
		}
	}
	m.Vector[core.NumWords-1] &= core.LastWordMask
	am.matrix = m
}

// Matrix returns the current materialized majority (memory) vector.
func (am *AssociativeMemory) Matrix() core.Hypervector {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.matrix
}

// Total returns the number of associations stored.
func (am *AssociativeMemory) Total() uint64 {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.total
}

// SaveToFile serializes the memory to disk through a writable memory-mapped file. On
// unix the fixed-size image is written directly into the mapping (no intermediate heap
// buffer); on other platforms a buffered fallback is used.
func (am *AssociativeMemory) SaveToFile(filename string) error {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return writeMappedFile(filename, memFileSize, am.encodeInto)
}

// LoadFromFile maps the file and loads the full memory state (counters, total, and matrix),
// leaving the instance ready for continued training.
func (am *AssociativeMemory) LoadFromFile(filename string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	data, closer, err := openMappedFile(filename)
	if err != nil {
		return fmt.Errorf("failed to open memory file: %w", err)
	}
	defer closer()

	if err := validateHeader(data); err != nil {
		return err
	}

	am.total = binary.LittleEndian.Uint64(data[16:])
	off := memHeader
	for k := 0; k < core.Dimension; k++ {
		am.counts[k] = binary.LittleEndian.Uint32(data[off+k*4:])
	}
	off += countsBytes
	for i := 0; i < core.NumWords; i++ {
		am.matrix.Vector[i] = binary.LittleEndian.Uint64(data[off+i*8:])
	}
	am.readOnly = false
	return nil
}

// OpenReadOnly maps the file and loads only the materialized matrix (skipping the 40 KB
// counter array), yielding a query-only memory for inference — the "zero-heap streaming"
// read path. StoreAssociation on the result panics.
func OpenReadOnly(filename string) (*AssociativeMemory, error) {
	data, closer, err := openMappedFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open memory file: %w", err)
	}
	defer closer()

	if err := validateHeader(data); err != nil {
		return nil, err
	}

	am := &AssociativeMemory{readOnly: true}
	am.total = binary.LittleEndian.Uint64(data[16:])
	off := memHeader + countsBytes
	for i := 0; i < core.NumWords; i++ {
		am.matrix.Vector[i] = binary.LittleEndian.Uint64(data[off+i*8:])
	}
	return am, nil
}

// encodeInto writes the full fixed-size file image into buf (len(buf) must be memFileSize).
// Caller must hold at least a read lock.
func (am *AssociativeMemory) encodeInto(buf []byte) {
	copy(buf[0:4], memMagic)
	binary.LittleEndian.PutUint16(buf[4:], memVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(core.Dimension))
	binary.LittleEndian.PutUint32(buf[12:], uint32(core.NumWords))
	binary.LittleEndian.PutUint64(buf[16:], am.total)

	off := memHeader
	for k := 0; k < core.Dimension; k++ {
		binary.LittleEndian.PutUint32(buf[off+k*4:], am.counts[k])
	}
	off += countsBytes
	for i := 0; i < core.NumWords; i++ {
		binary.LittleEndian.PutUint64(buf[off+i*8:], am.matrix.Vector[i])
	}
}

// validateHeader checks the magic, version, and dimension of a mapped file image.
func validateHeader(buf []byte) error {
	if len(buf) < memFileSize {
		return fmt.Errorf("memory file too small: %d bytes (want %d)", len(buf), memFileSize)
	}
	if string(buf[0:4]) != memMagic {
		return fmt.Errorf("bad magic %q (not a NeuroVSA memory file)", buf[0:4])
	}
	if v := binary.LittleEndian.Uint16(buf[4:]); v != memVersion {
		return fmt.Errorf("unsupported memory file version %d (want %d)", v, memVersion)
	}
	if d := binary.LittleEndian.Uint32(buf[8:]); d != uint32(core.Dimension) {
		return fmt.Errorf("dimension mismatch: file has %d, build has %d", d, core.Dimension)
	}
	return nil
}
