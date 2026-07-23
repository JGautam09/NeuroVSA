package engine

import (
	"os"
	"path/filepath"
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
