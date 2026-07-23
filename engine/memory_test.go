package engine

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// The counter-vector majority must equal an explicit Bundle of the same bound pairs (for an
// odd number of associations, where there are no majority ties).
func TestCounterMemoryMatchesBundle(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()

	pairs := [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}, {"g", "h"}, {"a", "x"}}
	var bounds []core.Hypervector
	for _, p := range pairs {
		c := dict.GetOrRegister(p[0])
		target := dict.GetOrRegister(p[1])
		mem.StoreAssociation(c, target)
		bounds = append(bounds, c.Bind(target))
	}

	if got, want := mem.Matrix(), core.Bundle(bounds); got != want {
		t.Fatalf("counter matrix != Bundle of bounds: hamming=%d", core.HammingDistance(got, want))
	}
	if mem.Total() != uint64(len(pairs)) {
		t.Fatalf("total = %d, want %d", mem.Total(), len(pairs))
	}
}

func TestMemoryMmapRoundTrip(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()

	seq := []string{"func", "main", "fmt.Println", "return", "nil"}
	prev := dict.GetOrRegister(seq[0])
	for _, tok := range seq[1:] {
		next := dict.GetOrRegister(tok)
		mem.StoreAssociation(prev, next)
		prev = prev.Permute(1).Bind(next)
	}

	path := filepath.Join(t.TempDir(), "mem.bin")
	if err := mem.SaveToFile(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Full load must reproduce matrix + total and allow continued training.
	loaded := NewAssociativeMemory()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Matrix() != mem.Matrix() {
		t.Fatalf("loaded matrix differs after mmap round-trip")
	}
	if loaded.Total() != mem.Total() {
		t.Fatalf("loaded total %d != %d", loaded.Total(), mem.Total())
	}
	loaded.StoreAssociation(dict.GetOrRegister("x"), dict.GetOrRegister("y")) // must not panic

	// Read-only open must serve the same matrix (loaded via mmap, counters skipped).
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	if ro.Matrix() != mem.Matrix() {
		t.Fatalf("read-only matrix differs from original")
	}
}

func TestOpenReadOnlyIsImmutable(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	mem.StoreAssociation(dict.GetOrRegister("a"), dict.GetOrRegister("b"))

	path := filepath.Join(t.TempDir(), "ro.bin")
	if err := mem.SaveToFile(path); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("expected StoreAssociation on a read-only memory to panic")
		}
	}()
	ro.StoreAssociation(dict.GetOrRegister("c"), dict.GetOrRegister("d"))
}

func TestLoadRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.bin")
	if err := os.WriteFile(path, []byte("definitely not a NeuroVSA memory image"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewAssociativeMemory().LoadFromFile(path); err == nil {
		t.Fatal("expected an error loading a malformed memory file")
	}
}

// v1 files (pre-ledger) must be rejected with an error that names the version problem.
func TestLoadRejectsV1Version(t *testing.T) {
	buf := make([]byte, memFixedSize+4)
	copy(buf[0:4], memMagic)
	binary.LittleEndian.PutUint16(buf[4:], 1) // version 1
	binary.LittleEndian.PutUint32(buf[8:], uint32(core.Dimension))
	binary.LittleEndian.PutUint32(buf[12:], uint32(core.NumWords))

	path := filepath.Join(t.TempDir(), "v1.bin")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	err := NewAssociativeMemory().LoadFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("expected a version error for a v1 file, got: %v", err)
	}
}

// Exact removal: after removing any subset (first, middle, last, all), the matrix must be
// bit-identical to a Bundle over exactly the remaining bound pairs — as if the removed
// associations had never been stored.
func TestExactRemovalMatchesBundle(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()

	pairs := [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}, {"g", "h"}, {"i", "j"}, {"k", "l"}, {"m", "n"}}
	bounds := make(map[AssociationID]core.Hypervector)
	var ids []AssociationID
	for _, p := range pairs {
		c, tgt := dict.GetOrRegister(p[0]), dict.GetOrRegister(p[1])
		id := mem.StoreAssociation(c, tgt)
		bounds[id] = c.Bind(tgt)
		ids = append(ids, id)
	}

	expectMatrix := func() {
		t.Helper()
		var remaining []core.Hypervector
		for _, id := range ids { // iterate ids to keep insertion order
			if hv, ok := bounds[id]; ok {
				remaining = append(remaining, hv)
			}
		}
		want := core.Bundle(remaining)
		if got := mem.Matrix(); got != want {
			t.Fatalf("matrix diverged from Bundle of remaining %d pairs: hamming=%d",
				len(remaining), core.HammingDistance(got, want))
		}
		if mem.Total() != uint64(len(remaining)) {
			t.Fatalf("total = %d, want %d", mem.Total(), len(remaining))
		}
	}

	for _, id := range []AssociationID{ids[0], ids[3], ids[6]} { // first, middle, last
		if err := mem.RemoveAssociation(id); err != nil {
			t.Fatalf("remove %d: %v", id, err)
		}
		delete(bounds, id)
		expectMatrix()
	}
	for id := range bounds { // drain to empty
		if err := mem.RemoveAssociation(id); err != nil {
			t.Fatalf("remove %d: %v", id, err)
		}
		delete(bounds, id)
	}
	expectMatrix()
	if !mem.Matrix().IsZero() {
		t.Fatal("empty memory matrix should be all zeros")
	}
}

func TestRemoveErrors(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	id := mem.StoreAssociation(dict.GetOrRegister("a"), dict.GetOrRegister("b"))

	if err := mem.RemoveAssociation(999); err == nil {
		t.Error("expected an error removing an unknown id")
	}
	if err := mem.RemoveAssociation(0); err == nil {
		t.Error("expected an error removing id 0")
	}
	if err := mem.RemoveAssociation(id); err != nil {
		t.Fatalf("first removal failed: %v", err)
	}
	if err := mem.RemoveAssociation(id); err == nil {
		t.Error("expected an error on double removal")
	}

	path := filepath.Join(t.TempDir(), "ro.bin")
	if err := mem.SaveToFile(path); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ro.RemoveAssociation(1); err == nil {
		t.Error("expected an error removing from a read-only memory")
	}
}

// The ledger must preserve labels and tombstones across a v2 save/load round-trip, and the
// reloaded memory must support continued exact removal.
func TestLedgerRoundTripV2(t *testing.T) {
	dict := core.NewSeededTokenDictionary(7)
	mem := NewAssociativeMemory()
	mem.SetVocabSeed(dict.Seed())

	type stored struct {
		id    AssociationID
		bound core.Hypervector
	}
	var entries []stored
	for i, p := range [][2]string{{"p", "q"}, {"r", "s"}, {"t", "u"}} {
		c, tgt := dict.GetOrRegister(p[0]), dict.GetOrRegister(p[1])
		id := mem.StoreLabeled(c, tgt, []string{"first", "second", "third"}[i])
		entries = append(entries, stored{id, c.Bind(tgt)})
	}
	if err := mem.RemoveAssociation(entries[1].id); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "mem_v2.bin")
	if err := mem.SaveToFile(path); err != nil {
		t.Fatal(err)
	}

	loaded := NewAssociativeMemory()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatal(err)
	}
	if loaded.VocabSeed() != 7 {
		t.Errorf("vocab seed = %d, want 7", loaded.VocabSeed())
	}
	if loaded.Total() != 2 {
		t.Errorf("total = %d, want 2", loaded.Total())
	}
	want := []AssociationRecord{
		{ID: 1, Label: "first"},
		{ID: 2, Label: "second", Removed: true},
		{ID: 3, Label: "third"},
	}
	got := loaded.Ledger()
	if len(got) != len(want) {
		t.Fatalf("ledger has %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ledger[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if ids := loaded.FindByLabel("second"); len(ids) != 0 {
		t.Errorf("FindByLabel must not return removed entries, got %v", ids)
	}
	if ids := loaded.FindByLabel("third"); len(ids) != 1 || ids[0] != 3 {
		t.Errorf("FindByLabel(third) = %v, want [3]", ids)
	}

	// Continued exact removal after reload.
	if err := loaded.RemoveAssociation(1); err != nil {
		t.Fatal(err)
	}
	if got, wantM := loaded.Matrix(), core.Bundle([]core.Hypervector{entries[2].bound}); got != wantM {
		t.Fatalf("post-reload removal diverged: hamming=%d", core.HammingDistance(got, wantM))
	}
}

// Contributors with maxDist 0 must name exactly the association whose (context, target) pair
// produced the probe, and nothing else.
func TestContributorsExactProvenance(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()

	c1, t1 := dict.GetOrRegister("ctx1"), dict.GetOrRegister("tgt1")
	c2, t2 := dict.GetOrRegister("ctx2"), dict.GetOrRegister("tgt2")
	id1 := mem.StoreLabeled(c1, t1, "assoc-one")
	mem.StoreLabeled(c2, t2, "assoc-two")

	got := mem.Contributors(c1.Bind(t1), 0)
	if len(got) != 1 || got[0].ID != id1 || got[0].Label != "assoc-one" {
		t.Fatalf("Contributors = %+v, want exactly [{%d assoc-one}]", got, id1)
	}
	if noise := mem.Contributors(core.GenerateRandom(), 0); len(noise) != 0 {
		t.Errorf("random probe matched %v, want none", noise)
	}
}
