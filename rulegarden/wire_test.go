package rulegarden

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWorldPackSeedSurvivesJSCycle: a world seed above 2^53 must survive a browser JSON
// round-trip, or deterministic replay diverges. Simulates JS by decoding through interface{}
// (numbers → float64) and re-encoding.
func TestWorldPackSeedSurvivesJSCycle(t *testing.T) {
	const bigSeed = uint64(12345678901234567890) // > 2^53
	w := NewWorld(bigSeed)
	if err := w.Apply(Event{Op: "spawn_creature", X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	blob, err := w.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"seed":"12345678901234567890"`) {
		t.Fatalf("world seed must be quoted on the wire, got: %s", blob)
	}

	// JS parse/stringify simulation.
	var generic any
	if err := json.Unmarshal(blob, &generic); err != nil {
		t.Fatal(err)
	}
	cycled, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ImportJSON(cycled)
	if err != nil {
		t.Fatal(err)
	}
	if back.Seed != bigSeed {
		t.Fatalf("JS cycle corrupted world seed: %d != %d", back.Seed, bigSeed)
	}
	if back.Hash() != w.Hash() {
		t.Fatal("world hash diverged after a simulated browser JSON round-trip")
	}
}
