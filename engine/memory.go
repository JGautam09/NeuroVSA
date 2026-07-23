package engine

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sync"

	"github.com/JGautam09/NeuroVSA/core"
)

// On-disk format (v2). The file is a fixed header + counters + matrix, followed by a
// variable-length association ledger:
//
//	[0:4]    magic "NVSA"
//	[4:6]    version (uint16, little-endian)
//	[6:8]    reserved / padding
//	[8:12]   dimension (uint32) — sanity check against the build
//	[12:16]  numWords  (uint32) — sanity check against the build
//	[16:24]  total     (uint64) — number of ACTIVE (non-removed) associations
//	[24:32]  vocabSeed (uint64) — seed of the TokenDictionary this memory was built against
//	[32 .. +countsBytes)  counts[Dimension] (uint32 LE) — the per-bit vote tally
//	[.. +matrixBytes)     matrix[NumWords]  (uint64 LE) — materialized majority vector
//	[..]     ledgerCount (uint32), then per entry:
//	         id (uint64) · removed (uint8) · labelLen (uint16) · label bytes · bound[NumWords] (uint64 LE)
//
// v1 files (no vocabSeed, no ledger) are rejected with a descriptive error — the project is
// pre-1.0 and v1 predates provenance, so there is nothing meaningful to migrate.
const (
	memMagic     = "NVSA"
	memVersion   = uint16(2)
	memHeader    = 32
	countsBytes  = core.Dimension * 4
	matrixBytes  = core.NumWords * 8
	memFixedSize = memHeader + countsBytes + matrixBytes
	maxLabelLen  = 65535
)

// RecommendedMaxActiveAssociations is the measured safe ceiling for ACTIVE associations in a
// single memory when recall happens over a small cleanup vocabulary (~4 candidates), as in a
// RuleGarden creature brain. The G0 capacity benchmark (engine/capacity_test.go, results in
// BENCHMARKS.md) shows 100% recall with comfortable margins through this count — quasi-
// orthogonal contexts hold 100% to K=256 and ~98.6% at K=512, and the full 96-percept
// structured space recalls perfectly. Callers that exceed this (e.g. after a NeuroMesh merge)
// should warn: recall degrades gracefully toward the noise floor, it does not fail loudly.
const RecommendedMaxActiveAssociations = 128

// AssociationID identifies one stored association. IDs are assigned sequentially from 1 and
// are never reused; removal tombstones the entry rather than compacting the ledger.
type AssociationID uint64

// AssociationRecord is the public view of one ledger entry.
type AssociationRecord struct {
	ID      AssociationID
	Label   string
	Removed bool
}

// Contributor names a ledger entry implicated in a prediction (see Contributors).
type Contributor struct {
	ID    AssociationID `json:"id"`
	Label string        `json:"label,omitempty"`
}

// ledgerEntry is the provenance record for one stored association. Invariant: the entry with
// ID i lives at ledger[i-1].
type ledgerEntry struct {
	label   string
	removed bool
	bound   core.Hypervector
}

// AssociativeMemory stores hypervector associations as a per-bit vote tally plus a
// provenance ledger.
//
// The tally (counts/total) makes every write O(D), independent of how many associations were
// stored before. The ledger records each association's bound vector and label — the price of
// provenance (~1.26 KB/association): it is what makes REMOVAL exact (you cannot delete what
// you cannot identify) and what lets traces name the association behind a prediction. Writes
// never re-bundle; the ledger is only consulted on removal and provenance queries.
type AssociativeMemory struct {
	mu        sync.RWMutex
	counts    [core.Dimension]uint32
	total     uint64
	matrix    core.Hypervector // cached majority vector, kept in sync on every store/remove
	readOnly  bool             // true for OpenReadOnly instances (counters/ledger not loaded)
	vocabSeed uint64           // metadata: item-memory seed the vocabulary was built with
	ledger    []ledgerEntry
}

// NewAssociativeMemory initializes an empty associative memory instance.
func NewAssociativeMemory() *AssociativeMemory {
	return &AssociativeMemory{vocabSeed: core.DefaultSeed}
}

// StoreAssociation binds a context vector with a target vector and folds it into the vote
// tally, recording an unlabeled ledger entry. Returns the association's ID. O(D).
func (am *AssociativeMemory) StoreAssociation(contextHV, targetHV core.Hypervector) AssociationID {
	return am.StoreLabeled(contextHV, targetHV, "")
}

// StoreLabeled is StoreAssociation with a human-readable provenance label (e.g.
// "fix_bug/step2→WriteFile"). Labels longer than 64 KiB are truncated.
func (am *AssociativeMemory) StoreLabeled(contextHV, targetHV core.Hypervector, label string) AssociationID {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.readOnly {
		panic("StoreLabeled called on a read-only (OpenReadOnly) memory")
	}
	if len(label) > maxLabelLen {
		label = label[:maxLabelLen]
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
	am.ledger = append(am.ledger, ledgerEntry{label: label, bound: bound})
	am.rematerializeLocked()
	return AssociationID(len(am.ledger))
}

// RemoveAssociation exactly unlearns one stored association: its bound vector's set bits are
// decremented from the tally, the active total drops by one, the entry is tombstoned, and the
// majority vector is rematerialized. The result is bit-identical to a memory that had never
// stored the entry. O(D), independent of corpus size — no retraining.
func (am *AssociativeMemory) RemoveAssociation(id AssociationID) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.readOnly {
		return fmt.Errorf("memory is read-only (OpenReadOnly)")
	}
	idx := int(id) - 1
	if idx < 0 || idx >= len(am.ledger) {
		return fmt.Errorf("unknown association id %d (ledger holds %d entries)", id, len(am.ledger))
	}
	entry := &am.ledger[idx]
	if entry.removed {
		return fmt.Errorf("association %d is already removed", id)
	}

	for i := 0; i < core.NumWords; i++ {
		w := entry.bound.Vector[i]
		base := i * 64
		for w != 0 {
			am.counts[base+bits.TrailingZeros64(w)]--
			w &= w - 1
		}
	}
	am.total--
	entry.removed = true
	am.rematerializeLocked()
	return nil
}

// Ledger returns the provenance records (id, label, removed) for every stored association,
// including tombstoned ones.
func (am *AssociativeMemory) Ledger() []AssociationRecord {
	am.mu.RLock()
	defer am.mu.RUnlock()

	out := make([]AssociationRecord, len(am.ledger))
	for i := range am.ledger {
		out[i] = AssociationRecord{ID: AssociationID(i + 1), Label: am.ledger[i].label, Removed: am.ledger[i].removed}
	}
	return out
}

// FindByLabel returns the ids of ACTIVE (non-removed) associations with an exactly matching
// label.
func (am *AssociativeMemory) FindByLabel(label string) []AssociationID {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var ids []AssociationID
	for i := range am.ledger {
		if !am.ledger[i].removed && am.ledger[i].label == label {
			ids = append(ids, AssociationID(i+1))
		}
	}
	return ids
}

// Contributors returns the active ledger entries whose bound vector lies within maxDist of
// probe. With probe = context ⊗ HV(prediction) and maxDist 0, this names the exact stored
// association that produced a prediction — the glass-box provenance behind cleanup results.
// O(N·NumWords) over the ledger. Empty for OpenReadOnly memories (ledger not loaded).
func (am *AssociativeMemory) Contributors(probe core.Hypervector, maxDist int) []Contributor {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var out []Contributor
	for i := range am.ledger {
		if am.ledger[i].removed {
			continue
		}
		if core.HammingDistance(am.ledger[i].bound, probe) <= maxDist {
			out = append(out, Contributor{ID: AssociationID(i + 1), Label: am.ledger[i].label})
		}
	}
	return out
}

// rematerializeLocked recomputes the cached majority vector from the counters, so it is
// bit-for-bit identical to core.Bundle over the ACTIVE bound pairs: a bit is set when
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

// Total returns the number of ACTIVE (non-removed) associations.
func (am *AssociativeMemory) Total() uint64 {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.total
}

// VocabSeed returns the item-memory seed this memory is annotated with.
func (am *AssociativeMemory) VocabSeed() uint64 {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.vocabSeed
}

// SetVocabSeed annotates the memory with the seed of the TokenDictionary it was built
// against, persisted in the file header so a reloading process can verify vocabulary
// compatibility.
func (am *AssociativeMemory) SetVocabSeed(seed uint64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.vocabSeed = seed
}

// fileSizeLocked computes the exact v2 image size for the current state.
func (am *AssociativeMemory) fileSizeLocked() int {
	size := memFixedSize + 4
	for i := range am.ledger {
		size += 8 + 1 + 2 + len(am.ledger[i].label) + matrixBytes
	}
	return size
}

// SaveToFile serializes the memory (tally, matrix, and full ledger) to disk through a
// writable memory-mapped file on unix, with a buffered fallback elsewhere.
func (am *AssociativeMemory) SaveToFile(filename string) error {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return writeMappedFile(filename, am.fileSizeLocked(), am.encodeInto)
}

// LoadFromFile maps the file and loads the full memory state (counters, total, vocab seed,
// matrix, and ledger), leaving the instance ready for continued training and exact removal.
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
	am.vocabSeed = binary.LittleEndian.Uint64(data[24:])
	off := memHeader
	for k := 0; k < core.Dimension; k++ {
		am.counts[k] = binary.LittleEndian.Uint32(data[off+k*4:])
	}
	off += countsBytes
	for i := 0; i < core.NumWords; i++ {
		am.matrix.Vector[i] = binary.LittleEndian.Uint64(data[off+i*8:])
	}
	off += matrixBytes

	if len(data) < off+4 {
		return fmt.Errorf("memory file truncated before ledger count")
	}
	count := int(binary.LittleEndian.Uint32(data[off:]))
	off += 4

	ledger := make([]ledgerEntry, 0, count)
	for n := 0; n < count; n++ {
		if len(data) < off+11 {
			return fmt.Errorf("memory file truncated in ledger entry %d", n+1)
		}
		id := binary.LittleEndian.Uint64(data[off:])
		if id != uint64(n+1) {
			return fmt.Errorf("corrupt ledger: entry %d carries id %d", n+1, id)
		}
		removed := data[off+8] != 0
		labelLen := int(binary.LittleEndian.Uint16(data[off+9:]))
		off += 11
		if len(data) < off+labelLen+matrixBytes {
			return fmt.Errorf("memory file truncated in ledger entry %d", n+1)
		}
		label := string(data[off : off+labelLen])
		off += labelLen
		var bound core.Hypervector
		for i := 0; i < core.NumWords; i++ {
			bound.Vector[i] = binary.LittleEndian.Uint64(data[off+i*8:])
		}
		off += matrixBytes
		ledger = append(ledger, ledgerEntry{label: label, removed: removed, bound: bound})
	}
	am.ledger = ledger
	am.readOnly = false
	return nil
}

// OpenReadOnly maps the file and loads only the vocab seed and materialized matrix (skipping
// the tally and ledger), yielding a query-only memory for inference — the zero-heap streaming
// read path. StoreLabeled panics and RemoveAssociation errors on the result; Contributors
// returns nothing (the ledger is not loaded).
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
	am.vocabSeed = binary.LittleEndian.Uint64(data[24:])
	off := memHeader + countsBytes
	for i := 0; i < core.NumWords; i++ {
		am.matrix.Vector[i] = binary.LittleEndian.Uint64(data[off+i*8:])
	}
	return am, nil
}

// encodeInto writes the full v2 file image into buf (len(buf) must equal fileSizeLocked()).
// Caller must hold at least a read lock.
func (am *AssociativeMemory) encodeInto(buf []byte) {
	copy(buf[0:4], memMagic)
	binary.LittleEndian.PutUint16(buf[4:], memVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(core.Dimension))
	binary.LittleEndian.PutUint32(buf[12:], uint32(core.NumWords))
	binary.LittleEndian.PutUint64(buf[16:], am.total)
	binary.LittleEndian.PutUint64(buf[24:], am.vocabSeed)

	off := memHeader
	for k := 0; k < core.Dimension; k++ {
		binary.LittleEndian.PutUint32(buf[off+k*4:], am.counts[k])
	}
	off += countsBytes
	for i := 0; i < core.NumWords; i++ {
		binary.LittleEndian.PutUint64(buf[off+i*8:], am.matrix.Vector[i])
	}
	off += matrixBytes

	binary.LittleEndian.PutUint32(buf[off:], uint32(len(am.ledger)))
	off += 4
	for n := range am.ledger {
		e := &am.ledger[n]
		binary.LittleEndian.PutUint64(buf[off:], uint64(n+1))
		if e.removed {
			buf[off+8] = 1
		} else {
			buf[off+8] = 0
		}
		binary.LittleEndian.PutUint16(buf[off+9:], uint16(len(e.label)))
		off += 11
		copy(buf[off:], e.label)
		off += len(e.label)
		for i := 0; i < core.NumWords; i++ {
			binary.LittleEndian.PutUint64(buf[off+i*8:], e.bound.Vector[i])
		}
		off += matrixBytes
	}
}

// validateHeader checks the magic, version, and dimension of a mapped file image.
func validateHeader(buf []byte) error {
	if len(buf) < memFixedSize {
		return fmt.Errorf("memory file too small: %d bytes (want at least %d)", len(buf), memFixedSize)
	}
	if string(buf[0:4]) != memMagic {
		return fmt.Errorf("bad magic %q (not a NeuroVSA memory file)", buf[0:4])
	}
	if v := binary.LittleEndian.Uint16(buf[4:]); v != memVersion {
		return fmt.Errorf("unsupported memory file version %d (this build reads v%d; v1 files predate the provenance ledger — re-train and re-save)", v, memVersion)
	}
	if d := binary.LittleEndian.Uint32(buf[8:]); d != uint32(core.Dimension) {
		return fmt.Errorf("dimension mismatch: file has %d, build has %d", d, core.Dimension)
	}
	return nil
}
