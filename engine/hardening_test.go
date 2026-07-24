package engine

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// P1a: an untrusted ledger count must be bounded by the file's byte budget, not allocated
// on faith — a tiny file claiming billions of entries must error, not attempt a huge alloc.
func TestLoadRejectsOversizedLedgerCount(t *testing.T) {
	buf := make([]byte, memFixedSize+4)
	copy(buf[0:4], memMagic)
	binary.LittleEndian.PutUint16(buf[4:], memVersion)
	binary.LittleEndian.PutUint32(buf[8:], uint32(core.Dimension))
	binary.LittleEndian.PutUint32(buf[12:], uint32(core.NumWords))
	binary.LittleEndian.PutUint32(buf[memHeader+countsBytes+matrixBytes:], 0xFFFFFFFF) // absurd count

	err := NewAssociativeMemory().UnmarshalBinary(buf)
	if err == nil || !strings.Contains(err.Error(), "byte budget") {
		t.Fatalf("expected a bounded-count error, got %v", err)
	}
}

// P1a: the fingerprint is ledger-derived, so a tampered serialized tally/matrix must not
// survive a load — the loader rebuilds both from the ledger. A load of a matrix-tampered
// image must reproduce the correct matrix, total, and fingerprint.
func TestLoadRebuildsStateFromLedgerIgnoringTamperedTally(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	mem.SetSite(3)
	var bounds []core.Hypervector
	for _, p := range [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}} {
		c, tg := dict.GetOrRegister(p[0]), dict.GetOrRegister(p[1])
		mem.StoreLabeled(c, tg, p[0]+"→"+p[1])
		bounds = append(bounds, c.Bind(tg))
	}
	wantMatrix := core.Bundle(bounds)
	wantFP := mem.Fingerprint()

	img, err := mem.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	// Poison the serialized total (header), counts, and matrix regions — everything the
	// loader must now distrust and rebuild.
	binary.LittleEndian.PutUint64(img[16:], 999) // fake total
	img[memHeader+4] ^= 0xFF                     // flip a counts byte
	img[memHeader+countsBytes+8] ^= 0xFF         // flip a matrix byte

	loaded := NewAssociativeMemory()
	if err := loaded.UnmarshalBinary(img); err != nil {
		t.Fatal(err)
	}
	if loaded.Total() != 3 {
		t.Fatalf("total not rebuilt from ledger: got %d, want 3", loaded.Total())
	}
	if loaded.Matrix() != wantMatrix {
		t.Fatalf("matrix not rebuilt from ledger (tampered bytes leaked): hamming=%d",
			core.HammingDistance(loaded.Matrix(), wantMatrix))
	}
	if loaded.Fingerprint() != wantFP {
		t.Fatal("fingerprint changed despite ledger being intact")
	}
}

// P2b: switching to a site that already holds merged entries must not mint a colliding ID.
func TestSetSiteAvoidsDuplicateIDs(t *testing.T) {
	dict := core.NewTokenDictionary()

	x := NewAssociativeMemory()
	x.SetSite(1)
	x.StoreLabeled(dict.GetOrRegister("a"), dict.GetOrRegister("b"), "x1") // {1,1}

	y := NewAssociativeMemory()
	y.SetSite(2)
	if _, err := y.Merge(x); err != nil { // y adopts {1,1}
		t.Fatal(err)
	}

	y.SetSite(1) // misuse-prone switch to a site with existing entries
	id := y.StoreLabeled(dict.GetOrRegister("c"), dict.GetOrRegister("d"), "y-new")
	if id == (AssociationID{Site: 1, Seq: 1}) {
		t.Fatalf("SetSite minted a colliding id %s", id)
	}
	if id.Seq <= 1 {
		t.Fatalf("nextSeq not advanced for the adopted site: %s", id)
	}

	// The ledger must stay consistent: save + load round-trips (a duplicate id would error).
	path := t.TempDir() + "/y.bin"
	if err := y.SaveToFile(path); err != nil {
		t.Fatalf("save after SetSite: %v", err)
	}
	if err := NewAssociativeMemory().LoadFromFile(path); err != nil {
		t.Fatalf("load after SetSite (duplicate id?): %v", err)
	}
}

// P2c: a pack whose label exceeds the 64 KiB serialization limit must be rejected before it
// can produce an unparseable memory image.
func TestPackRejectsOversizedLabel(t *testing.T) {
	p := Pack{
		Name:      "bad",
		Site:      1,
		VocabSeed: core.DefaultSeed,
		Entries:   []PackEntry{{Seq: 1, Label: strings.Repeat("x", maxLabelLen+1), Bound: core.GenerateRandom()}},
	}
	if _, err := p.Memory(); err == nil || !strings.Contains(err.Error(), "label") {
		t.Fatalf("expected an over-limit label error from Memory(), got %v", err)
	}
	if _, err := ApplyPack(NewAssociativeMemory(), &p); err == nil {
		t.Fatal("ApplyPack accepted an over-limit label")
	}
}

// P1b (engine layer): a certificate's executed action/basis are re-derived at verify time,
// so an instinct-style override (executed ≠ raw winner) verifies, and tampering the executed
// action fails.
func TestCertificateVerifiesExecutedAction(t *testing.T) {
	mem, state, tools := certFixture(t) // exact lesson: chosen=RunTests, decisive margin
	cert, err := IssueDecision(mem, state, tools, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Impose an instinct policy: any margin below Dimension counts as "no confident match",
	// so the executed action becomes the default (WriteFile) — different from the raw winner.
	cert.MinMargin = core.Dimension
	cert.InstinctAction = "WriteFile"
	cert.ExecutedAction, cert.Basis = DerivePolicyOutcome(mem.Total(), cert.Candidates, cert.Contributors, cert.MinMargin, cert.InstinctAction)
	if cert.Basis != "instinct" || cert.ExecutedAction != "WriteFile" || cert.ExecutedAction == cert.Chosen {
		t.Fatalf("policy derivation wrong: basis=%q executed=%q chosen=%q", cert.Basis, cert.ExecutedAction, cert.Chosen)
	}
	if res := cert.VerifyAgainst(mem); !res.OK() {
		t.Fatalf("policy-annotated certificate failed verification: %+v", res)
	}

	tampered := cert
	tampered.ExecutedAction = "ReadFile"
	if res := tampered.VerifyAgainst(mem); res.OK() {
		t.Fatal("tampered executed action passed verification")
	}
}
