package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// goldenTokens are the (seed, token) pairs pinned in testdata/golden_vectors.json. They cover
// plain words, namespaced tokens, an empty string, and non-ASCII input.
var goldenSeeds = []uint64{0, 42}
var goldenTokens = []string{"func", "main", "fix_bug", "ASTSearch", "param:x", "goal:deploy_service", "", "µ✓-token"}

type goldenEntry struct {
	Seed  uint64 `json:"seed"`
	Token string `json:"token"`
	Hex   string `json:"hex"` // 157 words, each %016x, concatenated
}

func hvToHex(hv Hypervector) string {
	out := make([]byte, 0, NumWords*16)
	for i := 0; i < NumWords; i++ {
		out = fmt.Appendf(out, "%016x", hv.Vector[i])
	}
	return string(out)
}

// TestSeededHVGolden pins SeededHV to committed golden vectors. CI running this on both
// ubuntu and macos runners is the standing proof of the README's cross-machine bit-identity
// claim: the golden file was generated on one machine and must match on every other.
// Regenerate deliberately with: UPDATE_GOLDEN=1 go test ./core/ -run TestSeededHVGolden
func TestSeededHVGolden(t *testing.T) {
	path := filepath.Join("testdata", "golden_vectors.json")

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		var entries []goldenEntry
		for _, seed := range goldenSeeds {
			for _, tok := range goldenTokens {
				entries = append(entries, goldenEntry{Seed: seed, Token: tok, Hex: hvToHex(SeededHV(seed, tok))})
			}
		}
		b, err := json.MarshalIndent(entries, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s with %d entries", path, len(entries))
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file missing (generate with UPDATE_GOLDEN=1): %v", err)
	}
	var entries []goldenEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(goldenSeeds)*len(goldenTokens) {
		t.Fatalf("golden file has %d entries, want %d", len(entries), len(goldenSeeds)*len(goldenTokens))
	}
	for _, e := range entries {
		if got := hvToHex(SeededHV(e.Seed, e.Token)); got != e.Hex {
			t.Errorf("SeededHV(%d, %q) diverged from golden vector", e.Seed, e.Token)
		}
	}
}

func TestSeededHVMaskedAndBalanced(t *testing.T) {
	for _, seed := range goldenSeeds {
		for _, tok := range goldenTokens {
			hv := SeededHV(seed, tok)
			if hv.Vector[NumWords-1]&^LastWordMask != 0 {
				t.Errorf("SeededHV(%d, %q): unmasked ghost bits in word 156", seed, tok)
			}
			if d := HammingDistance(hv, ZeroHV()); d < 4500 || d > 5500 {
				t.Errorf("SeededHV(%d, %q): bit density %d outside [4500, 5500]", seed, tok, d)
			}
		}
	}
}

// Distinct tokens under one seed, and one token under distinct seeds, must be quasi-orthogonal.
func TestSeededHVDecorrelates(t *testing.T) {
	if d := HammingDistance(SeededHV(0, "func"), SeededHV(0, "main")); d < 4500 || d > 5500 {
		t.Errorf("distinct tokens: d_H=%d outside [4500, 5500]", d)
	}
	if d := HammingDistance(SeededHV(0, "func"), SeededHV(42, "func")); d < 4500 || d > 5500 {
		t.Errorf("distinct seeds: d_H=%d outside [4500, 5500]", d)
	}
}

// Two independently built default dictionaries must assign bit-identical vectors — the
// property that makes persisted memories usable across process restarts.
func TestDictionaryDeterministicByDefault(t *testing.T) {
	d1 := NewTokenDictionary()
	d2 := NewTokenDictionary()
	for _, tok := range goldenTokens {
		if d1.GetOrRegister(tok) != d2.GetOrRegister(tok) {
			t.Errorf("default dictionaries disagree on %q", tok)
		}
	}
	if d1.Seed() != DefaultSeed {
		t.Errorf("default dictionary seed = %d, want DefaultSeed", d1.Seed())
	}
}

// The legacy random mode must still produce unpredictable (per-instance) vectors.
func TestRandomDictionaryIsNotReproducible(t *testing.T) {
	r1 := NewRandomTokenDictionary()
	r2 := NewRandomTokenDictionary()
	if r1.GetOrRegister("func") == r2.GetOrRegister("func") {
		t.Error("two random dictionaries produced identical vectors (astronomically unlikely)")
	}
}
