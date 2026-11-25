package engine

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/JGautam09/NeuroVSA/core"
)

// Pack is a distributable, signable set of labeled associations — lessons or workflow steps
// — that installs and revokes AS A UNIT.
//
// Design: a pack IS a mini-replica. Its entries carry fixed (Site, Seq) identities chosen by
// the pack author, and ApplyPack materializes the pack as a memory and MERGES it. That one
// decision buys every NeuroMesh guarantee for free: two players applying the same pack end
// up with identical entries (merging their worlds later deduplicates instead of
// double-counting), applying a pack twice is idempotent, and RevokePack's tombstones
// propagate to everyone downstream — revocation travels with the data.
type Pack struct {
	Name      string      `json:"-"` // JSON via custom marshalers below
	VocabSeed uint64      `json:"-"`
	Site      uint64      `json:"-"` // the pack's writer identity; authors must claim distinct sites
	Entries   []PackEntry `json:"-"`
	PublicKey []byte      `json:"-"`
	Signature []byte      `json:"-"`
}

// PackEntry is one association: its sequence under the pack's site, its human-readable
// label, its optional machine-readable semantics record, and the bound vector
// (context ⊗ target) it contributes to the memory. Sem is what import validation checks
// against Bound (see rulegarden's validateLessonPackContent) — the label is display-only.
type PackEntry struct {
	Seq   uint64           `json:"seq"`
	Label string           `json:"label"`
	Sem   string           `json:"sem,omitempty"`
	Bound core.Hypervector `json:"-"` // hex in JSON (see marshalers)
}

// PackFromMemory captures a memory's ACTIVE associations as a pack. The natural authoring
// flow: claim a site, teach lessons into a scratch memory, export, sign, distribute.
func PackFromMemory(name string, mem *AssociativeMemory) Pack {
	mem.mu.RLock()
	defer mem.mu.RUnlock()

	p := Pack{Name: name, VocabSeed: mem.vocabSeed, Site: mem.site}
	for _, e := range mem.canonicalLocked() {
		if e.removed {
			continue
		}
		p.Entries = append(p.Entries, PackEntry{Seq: e.id.Seq, Label: e.label, Sem: e.sem, Bound: e.bound})
	}
	return p
}

// PacksFromMemory captures a memory's ACTIVE associations as one single-site pack per
// writer site, in the memory's canonical entry order. This is the lossless snapshot form
// for multi-site memories: a Pack is a single-site mini-replica by design, so a brain that
// has merged lessons from several authors must ship one pack per author — each lesson keeps
// its original (site, seq) identity, which is what makes re-applying idempotent and
// cross-replica deduplication exact. (PackFromMemory remains the single-author flow; it
// exports under the memory's OWN site and is only lossless for single-site memories.)
func PacksFromMemory(name string, mem *AssociativeMemory) []Pack {
	mem.mu.RLock()
	defer mem.mu.RUnlock()

	bySite := make(map[uint64]*Pack)
	var order []uint64 // first-appearance order of sites in canonical entry order
	for _, e := range mem.canonicalLocked() {
		if e.removed {
			continue
		}
		p := bySite[e.id.Site]
		if p == nil {
			p = &Pack{Name: name, VocabSeed: mem.vocabSeed, Site: e.id.Site}
			bySite[e.id.Site] = p
			order = append(order, e.id.Site)
		}
		p.Entries = append(p.Entries, PackEntry{Seq: e.id.Seq, Label: e.label, Sem: e.sem, Bound: e.bound})
	}
	packs := make([]Pack, 0, len(order))
	for _, s := range order {
		packs = append(packs, *bySite[s])
	}
	return packs
}

// Memory materializes the pack as its mini-replica: a memory at the pack's site containing
// exactly the pack's entries.
func (p *Pack) Memory() (*AssociativeMemory, error) {
	m := NewAssociativeMemory()
	m.vocabSeed = p.VocabSeed
	m.site = p.Site
	maxSeq := uint64(0)
	for _, e := range p.Entries {
		if len(e.Label) > maxLabelLen {
			return nil, fmt.Errorf("pack %q entry seq %d has a %d-byte label exceeding the %d-byte limit", p.Name, e.Seq, len(e.Label), maxLabelLen)
		}
		if len(e.Sem) > MaxSemLen {
			return nil, fmt.Errorf("pack %q entry seq %d has a %d-byte sem exceeding the %d-byte limit", p.Name, e.Seq, len(e.Sem), MaxSemLen)
		}
		if err := e.Bound.ValidateCanonical(); err != nil {
			return nil, fmt.Errorf("pack %q entry seq %d: %w", p.Name, e.Seq, err)
		}
		id := AssociationID{Site: p.Site, Seq: e.Seq}
		if _, dup := m.index[id]; dup {
			return nil, fmt.Errorf("pack %q has duplicate entry seq %d", p.Name, e.Seq)
		}
		m.index[id] = len(m.ledger)
		m.ledger = append(m.ledger, ledgerEntry{id: id, label: e.Label, sem: e.Sem, bound: e.Bound})
		m.addBits(e.Bound)
		m.total++
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
	}
	m.nextSeq = maxSeq + 1
	m.rematerializeLocked()
	return m, nil
}

// ApplyPack installs the pack into target via a NeuroMesh merge — idempotent, dedupes with
// any replica that applied the same pack, refuses vocab mismatches and site collisions.
func ApplyPack(target *AssociativeMemory, p *Pack) (MergeReport, error) {
	pm, err := p.Memory()
	if err != nil {
		return MergeReport{}, err
	}
	return target.Merge(pm)
}

// RevokePack tombstones every pack entry present in target, as a unit. Already-removed and
// never-installed entries are skipped. The tombstones propagate through subsequent merges —
// a revoked pack stays revoked everywhere the data travels. Returns how many associations
// were removed here.
func RevokePack(target *AssociativeMemory, p *Pack) int {
	removed := 0
	for _, e := range p.Entries {
		if err := target.RemoveAssociation(AssociationID{Site: p.Site, Seq: e.Seq}); err == nil {
			removed++
		}
	}
	return removed
}

// Sign binds the pack to an ed25519 key over its canonical encoding.
func (p *Pack) Sign(priv ed25519.PrivateKey) {
	p.PublicKey = append([]byte(nil), priv.Public().(ed25519.PublicKey)...)
	p.Signature = ed25519.Sign(priv, p.CanonicalBytes())
}

// VerifySignature checks the embedded signature over the canonical encoding.
func (p *Pack) VerifySignature() bool {
	if len(p.PublicKey) != ed25519.PublicKeySize || len(p.Signature) == 0 {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(p.PublicKey), p.CanonicalBytes(), p.Signature)
}

// CanonicalBytes is the deterministic binary encoding signatures cover (PublicKey and
// Signature excluded). Two formats, selected by content: a pack whose entries carry no
// sem encodes exactly as it always has ("NVSA-PACK1"), so every signature issued before
// the sem field existed keeps verifying; any entry with a sem switches the whole pack to
// "NVSA-PACK2", which writes a sem string per entry. The format tag is itself inside the
// signed bytes, so a v2 pack stripped of its sems (or a v1 pack with sems injected)
// re-canonicalizes as the OTHER format and fails its signature — the two forms cannot
// alias each other.
func (p *Pack) CanonicalBytes() []byte {
	withSem := false
	for i := range p.Entries {
		if p.Entries[i].Sem != "" {
			withSem = true
			break
		}
	}

	var b bytes.Buffer
	if withSem {
		b.WriteString("NVSA-PACK2")
	} else {
		b.WriteString("NVSA-PACK1")
	}
	writeStr := func(s string) {
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(s)))
		b.Write(l[:])
		b.WriteString(s)
	}
	writeU64 := func(v uint64) {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], v)
		b.Write(buf[:])
	}

	writeStr(p.Name)
	writeU64(p.VocabSeed)
	writeU64(p.Site)
	writeU64(uint64(len(p.Entries)))
	for _, e := range p.Entries {
		writeU64(e.Seq)
		writeStr(e.Label)
		if withSem {
			writeStr(e.Sem)
		}
		for i := 0; i < core.NumWords; i++ {
			writeU64(e.Bound.Vector[i])
		}
	}
	return b.Bytes()
}

// ---- JSON (bound vectors as hex; 64-bit ids as strings) ----

// All 64-bit ids on the wire are core.QuotedU64 (decimal strings) — see that type for why
// JavaScript makes this mandatory. The canonical *signing* bytes still use raw binary
// uint64, so quoting the JSON changes nothing a signature covers.
type packWireJSON struct {
	Name      string         `json:"name"`
	VocabSeed core.QuotedU64 `json:"vocab_seed"`
	Site      core.QuotedU64 `json:"site"`
	Entries   []PackEntry    `json:"entries"`
	PublicKey []byte         `json:"public_key,omitempty"`
	Signature []byte         `json:"signature,omitempty"`
}

func (p Pack) MarshalJSON() ([]byte, error) {
	return json.Marshal(packWireJSON{
		Name:      p.Name,
		VocabSeed: core.QuotedU64(p.VocabSeed),
		Site:      core.QuotedU64(p.Site),
		Entries:   p.Entries,
		PublicKey: p.PublicKey,
		Signature: p.Signature,
	})
}

func (p *Pack) UnmarshalJSON(data []byte) error {
	var w packWireJSON
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	p.Name, p.VocabSeed, p.Site = w.Name, uint64(w.VocabSeed), uint64(w.Site)
	p.Entries, p.PublicKey, p.Signature = w.Entries, w.PublicKey, w.Signature
	return nil
}

type packEntryJSON struct {
	Seq      core.QuotedU64 `json:"seq"`
	Label    string         `json:"label"`
	Sem      string         `json:"sem,omitempty"`
	BoundHex string         `json:"bound_hex"`
}

func (e PackEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal(packEntryJSON{Seq: core.QuotedU64(e.Seq), Label: e.Label, Sem: e.Sem, BoundHex: hvToHex(e.Bound)})
}

func (e *PackEntry) UnmarshalJSON(data []byte) error {
	var j packEntryJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	hv, err := hvFromHex(j.BoundHex)
	if err != nil {
		return fmt.Errorf("bound_hex: %w", err)
	}
	e.Seq, e.Label, e.Sem, e.Bound = uint64(j.Seq), j.Label, j.Sem, hv
	return nil
}
