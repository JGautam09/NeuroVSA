package engine

import (
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// trainDemoChain trains the canonical demo sequence func -> main -> fmt.Println -> return ->
// nil using the same recurrence the API server uses.
func trainDemoChain() (*core.TokenDictionary, *HDCDecoder) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	dec := NewHDCDecoder(mem, dict)

	for _, tok := range []string{"func", "main", "fmt.Println", "return", "nil", "if", "for", "select"} {
		dict.GetOrRegister(tok)
	}

	prev := dict.GetOrRegister("func")
	for _, tok := range []string{"main", "fmt.Println", "return", "nil"} {
		next := dict.GetOrRegister(tok)
		mem.StoreAssociation(prev, next)
		prev = prev.Permute(1).Bind(next)
	}
	return dict, dec
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Both a single-word and a multi-word seed must recover the correct continuation via the
// unified EncodeContext, and generation must stop at the end of the chain rather than
// padding with noise tokens.
func TestGenerateSequenceUnifiedEncoding(t *testing.T) {
	dict, dec := trainDemoChain()

	got1, _ := dec.GenerateSequence(EncodeContext(dict, []string{"func"}), 8)
	if want := []string{"main", "fmt.Println", "return", "nil"}; !equalStrings(got1, want) {
		t.Errorf("seed 'func' -> %v, want %v", got1, want)
	}

	// "func main" is two tokens in, so the correct continuation starts at fmt.Println.
	got2, _ := dec.GenerateSequence(EncodeContext(dict, []string{"func", "main"}), 8)
	if want := []string{"fmt.Println", "return", "nil"}; !equalStrings(got2, want) {
		t.Errorf("seed 'func main' -> %v, want %v", got2, want)
	}
}

// The confidence stop must prevent noise padding: with a tiny maxTokens budget the chain
// still stops at the trained boundary.
func TestGenerateSequenceStopsAtNoiseFloor(t *testing.T) {
	dict, dec := trainDemoChain()
	got, dists := dec.GenerateSequence(EncodeContext(dict, []string{"func"}), 32)
	if len(got) != 4 {
		t.Fatalf("expected generation to stop after 4 tokens, got %d: %v", len(got), got)
	}
	for i, d := range dists {
		if norm := float64(d) / float64(core.Dimension); norm > dec.StopThreshold {
			t.Errorf("emitted token %q at index %d exceeded stop threshold: %.3f", got[i], i, norm)
		}
	}
}
