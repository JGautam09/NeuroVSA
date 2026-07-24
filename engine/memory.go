package engine

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
	"sync"

	"github.com/JGautam09/NeuroVSA/core"
)

// On-disk format (v3). The file is a fixed header + counters + matrix, followed by a
// variable-length association ledger in canonical (site, seq) order:
//
//	[0:4]    magic "NVSA"
//	[4:6]    version (uint16, little-endian)
//	[6:8]    reserved / padding
//	[8:12]   dimension (uint32) — sanity check against the build
//	[12:16]  numWords  (uint32) — sanity check against the build
//	[16:24]  total     (uint64) — number of ACTIVE (non-removed) associations
//	[24:32]  vocabSeed (uint64) — seed of the TokenDictionary this memory was built against
//	[32:40]  site      (uint64) — this memory's writer identity (NEW in v3)
//	[40 .. +countsBytes)  counts[Dimension] (uint32 LE) — the per-bit vote tally
//	[.. +matrixBytes)     matrix[NumWords]  (uint64 LE) — materialized majority vector
//	[..]     ledgerCount (uint32), then per entry:
//	         site (u64) · seq (u64) · removed (u8) · labelLen (u16) · label bytes · bound[NumWords] (u64 LE)
//
// v1/v2 files are rejected with a descriptive error — pre-1.0, and they predate site-scoped
// identity, so there is nothing meaningful to migrate.
const (
	memMagic     = "NVSA"
	memVersion   = uint16(3)
	memHeader    = 40
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
// structured space recalls perfectly. Callers that exceed this (e.g. after a merge) should
// warn: recall degrades gracefully toward the noise floor, it does not fail loudly.
const RecommendedMaxActiveAssociations = 128

// AssociationID identifies one stored association globally: Site is the writer's identity,
// Seq that writer's monotonic counter. Composite identity is what makes memories MERGEABLE —
// two players' lessons can never collide as long as they write under different sites. The
// text form is "site:seq" (also used in JSON), and IDs are never reused: removal tombstones
// the entry rather than compacting the ledger.
type AssociationID struct {
	Site uint64
	Seq  uint64
}

func (id AssociationID) String() string {
	return fmt.Sprintf("%d:%d", id.Site, id.Seq)
}

// MarshalText / UnmarshalText give IDs the "site:seq" wire form in JSON and map keys.
func (id AssociationID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id *AssociationID) UnmarshalText(b []byte) error {
	var site, seq uint64
	if _, err := fmt.Sscanf(string(b), "%d:%d", &site, &seq); err != nil {
		return fmt.Errorf("invalid association id %q (want \"site:seq\")", b)
	}
	id.Site, id.Seq = site, seq
	return nil
}

func (id AssociationID) less(other AssociationID) bool {
	if id.Site != other.Site {
		return id.Site < other.Site
	}
	return id.Seq < other.Seq
}

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

// ledgerEntry is the provenance record for one stored association.
type ledgerEntry struct {
	id      AssociationID
	label   string
	removed bool
	bound   core.Hypervector
}

// AssociativeMemory stores hypervector associations as a per-bit vote tally plus a
// provenance ledger — and since v0.3.0 the ledger IS the source of truth: a two-phase set
// (grow-only entries + monotone tombstones) keyed by globally unique (site, seq) IDs, with
// the tally and matrix as a materialized view. That structure is what makes memories
// mergeable with CRDT guarantees (see Merge): union is commutative, associative, and
// idempotent by construction.
type AssociativeMemory struct {
	mu        sync.RWMutex
	counts    [core.Dimension]uint32
	total     uint64
	matrix    core.Hypervector // cached majority vector, kept in sync on every mutation
	readOnly  bool             // true for OpenReadOnly instances (counters/ledger not loaded)
	vocabSeed uint64           // metadata: item-memory seed the vocabulary was built with
	site      uint64           // writer identity for new associations
	nextSeq   uint64           // this writer's monotonic sequence (starts at 1)
	ledger    []ledgerEntry    // append order; canonical (site, seq) order is derived on demand
	index     map[AssociationID]int
}

// NewAssociativeMemory initializes an empty associative memory at site 0. Writers that
// intend to merge with peers must claim a distinct site via SetSite BEFORE storing.
func NewAssociativeMemory() *AssociativeMemory {
	return &AssociativeMemory{
		vocabSeed: core.DefaultSeed,
		nextSeq:   1,
		index:     make(map[AssociationID]int),
	}
}

// Site returns this memory's writer identity.
func (am *AssociativeMemory) Site() uint64 {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.site
}

// SetSite claims a writer identity for subsequently stored associations. Distinct writers
// MUST use distinct sites — the merge contract detects same-ID/different-content collisions
// and refuses them, but cannot repair them.
//
// nextSeq is recomputed for the chosen site by scanning the ledger, so switching to a site
// that already has entries (e.g. one adopted through a merge) cannot mint a colliding ID.
func (am *AssociativeMemory) SetSite(site uint64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.site = site
	maxSeq := uint64(0)
	for i := range am.ledger {
		if am.ledger[i].id.Site == site && am.ledger[i].id.Seq > maxSeq {
			maxSeq = am.ledger[i].id.Seq
		}
	}
	am.nextSeq = maxSeq + 1
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
	am.addBits(bound)
	am.total++

	id := AssociationID{Site: am.site, Seq: am.nextSeq}
	am.nextSeq++
	am.index[id] = len(am.ledger)
	am.ledger = append(am.ledger, ledgerEntry{id: id, label: label, bound: bound})
	am.rematerializeLocked()
	return id
}

// RemoveAssociation exactly unlearns one stored association: its bound vector's set bits are
// decremented from the tally, the active total drops by one, the entry is tombstoned, and the
// majority vector is rematerialized. The result is bit-identical to a memory that had never
// stored the entry. Tombstones are permanent and PROPAGATE through merges — forgetting wins.
func (am *AssociativeMemory) RemoveAssociation(id AssociationID) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.readOnly {
		return fmt.Errorf("memory is read-only (OpenReadOnly)")
	}
	idx, ok := am.index[id]
	if !ok {
		return fmt.Errorf("unknown association id %s (ledger holds %d entries)", id, len(am.ledger))
	}
	entry := &am.ledger[idx]
	if entry.removed {
		return fmt.Errorf("association %s is already removed", id)
	}

	am.subBits(entry.bound)
	am.total--
	entry.removed = true
	am.rematerializeLocked()
	return nil
}

// Ledger returns the provenance records (id, label, removed) for every stored association,
// including tombstoned ones, in canonical (site, seq) order.
func (am *AssociativeMemory) Ledger() []AssociationRecord {
	am.mu.RLock()
	defer am.mu.RUnlock()

	out := make([]AssociationRecord, len(am.ledger))
	for i, e := range am.canonicalLocked() {
		out[i] = AssociationRecord{ID: e.id, Label: e.label, Removed: e.removed}
	}
	return out
}

// FindByLabel returns the ids of ACTIVE (non-removed) associations with an exactly matching
// label, in canonical order.
func (am *AssociativeMemory) FindByLabel(label string) []AssociationID {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var ids []AssociationID
	for _, e := range am.canonicalLocked() {
		if !e.removed && e.label == label {
			ids = append(ids, e.id)
		}
	}
	return ids
}

// Contributors returns the active ledger entries whose bound vector lies within maxDist of
// probe, in canonical order. With probe = context ⊗ HV(prediction) and maxDist 0, this names
// the exact stored association that produced a prediction. Empty for OpenReadOnly memories
// (ledger not loaded).
func (am *AssociativeMemory) Contributors(probe core.Hypervector, maxDist int) []Contributor {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var out []Contributor
	for _, e := range am.canonicalLocked() {
		if e.removed {
			continue
		}
		if core.HammingDistance(e.bound, probe) <= maxDist {
			out = append(out, Contributor{ID: e.id, Label: e.label})
		}
	}
	return out
}

// addBits / subBits fold one bound vector into / out of the tally. O(D).
func (am *AssociativeMemory) addBits(bound core.Hypervector) {
	for i := 0; i < core.NumWords; i++ {
		w := bound.Vector[i]
		base := i * 64
		for w != 0 {
			am.counts[base+bits.TrailingZeros64(w)]++
			w &= w - 1
		}
	}
}

func (am *AssociativeMemory) subBits(bound core.Hypervector) {
	for i := 0; i < core.NumWords; i++ {
		w := bound.Vector[i]
		base := i * 64
		for w != 0 {
			am.counts[base+bits.TrailingZeros64(w)]--
			w &= w - 1
		}
	}
}

// rematerializeLocked recomputes the cached majority vector from the counters, so it is
// bit-for-bit identical to core.Bundle over the ACTIVE bound pairs: a bit is set when
// strictly more than half voted for it, and exact ties (even total, counts*2 == total) use
// the same deterministic tie-break as Bundle.
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

// canonicalLocked returns the ledger entries sorted by (site, seq). Canonical order is what
// serialization, fingerprints, and public views use, so replicas that merged in different
// orders expose identical state. Caller must hold at least a read lock.
func (am *AssociativeMemory) canonicalLocked() []*ledgerEntry {
	out := make([]*ledgerEntry, len(am.ledger))
	for i := range am.ledger {
		out[i] = &am.ledger[i]
	}
	sort.Slice(out, func(a, b int) bool { return out[a].id.less(out[b].id) })
	return out
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
// against. Merging requires equal vocab seeds — vectors must mean the same thing.
func (am *AssociativeMemory) SetVocabSeed(seed uint64) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.vocabSeed = seed
}

// fileSizeLocked computes the exact v3 image size for the current state.
func (am *AssociativeMemory) fileSizeLocked() int {
	size := memFixedSize + 4
	for i := range am.ledger {
		size += 8 + 8 + 1 + 2 + len(am.ledger[i].label) + matrixBytes
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

// MarshalBinary returns the memory's complete v3 image — byte-identical to a saved file.
// Used for in-process clones (atomic multi-merge), wasm exports (no filesystem in the
// browser), and receipt+brain bundles for nvsa-verify.
func (am *AssociativeMemory) MarshalBinary() ([]byte, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()
	buf := make([]byte, am.fileSizeLocked())
	am.encodeInto(buf)
	return buf, nil
}

// UnmarshalBinary restores a memory from a v3 image (see MarshalBinary). The receiver is
// fully overwritten.
func (am *AssociativeMemory) UnmarshalBinary(data []byte) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	return am.loadFromImageLocked(data)
}

// LoadFromFile maps the file and loads the full memory state, leaving the instance ready for
// continued training, exact removal, and merging. The writer's sequence resumes after the
// highest own-site entry so future IDs never collide with loaded history.
func (am *AssociativeMemory) LoadFromFile(filename string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	data, closer, err := openMappedFile(filename)
	if err != nil {
		return fmt.Errorf("failed to open memory file: %w", err)
	}
	defer closer()
	return am.loadFromImageLocked(data)
}

// minLedgerEntryBytes is the smallest possible serialized ledger entry (site + seq + removed
// + labelLen + bound vector, zero-length label). Used to bound an untrusted ledger count
// against the remaining file size before allocating.
const minLedgerEntryBytes = 8 + 8 + 1 + 2 + matrixBytes

// loadFromImageLocked parses a complete v3 image into local state, rebuilds the vote tally
// and majority vector FROM the parsed ledger (never trusting the serialized copies — the
// fingerprint anchor is ledger-derived, so a tampered tally/matrix must not survive), and
// commits atomically only on success. Caller must hold the write lock.
//
// Hardened against malformed input: the ledger count is bounded by the remaining byte budget
// before allocation, and every field read is length-checked. A parse failure leaves the
// receiver unchanged (no partial mutation).
func (am *AssociativeMemory) loadFromImageLocked(data []byte) error {
	if err := validateHeader(data); err != nil {
		return err
	}

	vocabSeed := binary.LittleEndian.Uint64(data[24:])
	site := binary.LittleEndian.Uint64(data[32:])

	// Skip the serialized counts+matrix: they are metadata for OpenReadOnly's zero-heap
	// path, but on a full load we rebuild them from the ledger so a tampered matrix cannot
	// influence a decision that still matches the (ledger-derived) fingerprint.
	off := memHeader + countsBytes + matrixBytes

	if len(data) < off+4 {
		return fmt.Errorf("memory file truncated before ledger count")
	}
	count := int(binary.LittleEndian.Uint32(data[off:]))
	off += 4
	if remaining := len(data) - off; count > remaining/minLedgerEntryBytes {
		return fmt.Errorf("ledger count %d exceeds the file's byte budget (%d bytes remain)", count, remaining)
	}

	ledger := make([]ledgerEntry, 0, count)
	index := make(map[AssociationID]int, count)
	maxOwnSeq := uint64(0)
	for n := 0; n < count; n++ {
		if len(data) < off+19 {
			return fmt.Errorf("memory file truncated in ledger entry %d", n+1)
		}
		id := AssociationID{
			Site: binary.LittleEndian.Uint64(data[off:]),
			Seq:  binary.LittleEndian.Uint64(data[off+8:]),
		}
		removed := data[off+16] != 0
		labelLen := int(binary.LittleEndian.Uint16(data[off+17:]))
		off += 19
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
		if _, dup := index[id]; dup {
			return fmt.Errorf("corrupt ledger: duplicate id %s", id)
		}
		index[id] = len(ledger)
		ledger = append(ledger, ledgerEntry{id: id, label: label, removed: removed, bound: bound})
		if id.Site == site && id.Seq > maxOwnSeq {
			maxOwnSeq = id.Seq
		}
	}

	// Rebuild the authoritative tally + total from the ACTIVE ledger entries.
	var counts [core.Dimension]uint32
	var total uint64
	for i := range ledger {
		if ledger[i].removed {
			continue
		}
		w := ledger[i].bound
		for j := 0; j < core.NumWords; j++ {
			word := w.Vector[j]
			base := j * 64
			for word != 0 {
				counts[base+bits.TrailingZeros64(word)]++
				word &= word - 1
			}
		}
		total++
	}

	// Commit atomically.
	am.vocabSeed = vocabSeed
	am.site = site
	am.counts = counts
	am.total = total
	am.ledger = ledger
	am.index = index
	am.nextSeq = maxOwnSeq + 1
	am.readOnly = false
	am.rematerializeLocked()
	return nil
}

// OpenReadOnly maps the file and loads only the metadata and materialized matrix (skipping
// the tally and ledger), yielding a query-only memory for inference. StoreLabeled panics,
// RemoveAssociation and Merge error, and Contributors returns nothing on the result.
func OpenReadOnly(filename string) (*AssociativeMemory, error) {
	data, closer, err := openMappedFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open memory file: %w", err)
	}
	defer closer()

	if err := validateHeader(data); err != nil {
		return nil, err
	}

	am := &AssociativeMemory{readOnly: true, index: make(map[AssociationID]int)}
	am.total = binary.LittleEndian.Uint64(data[16:])
	am.vocabSeed = binary.LittleEndian.Uint64(data[24:])
	am.site = binary.LittleEndian.Uint64(data[32:])
	off := memHeader + countsBytes
	for i := 0; i < core.NumWords; i++ {
		am.matrix.Vector[i] = binary.LittleEndian.Uint64(data[off+i*8:])
	}
	return am, nil
}

// encodeInto writes the full v3 file image into buf (len(buf) must equal fileSizeLocked()).
// Entries are written in canonical order so identical states produce identical bytes.
// Caller must hold at least a read lock.
func (am *AssociativeMemory) encodeInto(buf []byte) {
	copy(buf[0:4], memMagic)
	binary.LittleEndian.PutUint16(buf[4:], memVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(core.Dimension))
	binary.LittleEndian.PutUint32(buf[12:], uint32(core.NumWords))
	binary.LittleEndian.PutUint64(buf[16:], am.total)
	binary.LittleEndian.PutUint64(buf[24:], am.vocabSeed)
	binary.LittleEndian.PutUint64(buf[32:], am.site)

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
	for _, e := range am.canonicalLocked() {
		binary.LittleEndian.PutUint64(buf[off:], e.id.Site)
		binary.LittleEndian.PutUint64(buf[off+8:], e.id.Seq)
		if e.removed {
			buf[off+16] = 1
		} else {
			buf[off+16] = 0
		}
		binary.LittleEndian.PutUint16(buf[off+17:], uint16(len(e.label)))
		off += 19
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
		return fmt.Errorf("unsupported memory file version %d (this build reads v%d; v1/v2 files predate site-scoped identity — re-train and re-save)", v, memVersion)
	}
	if d := binary.LittleEndian.Uint32(buf[8:]); d != uint32(core.Dimension) {
		return fmt.Errorf("dimension mismatch: file has %d, build has %d", d, core.Dimension)
	}
	return nil
}
