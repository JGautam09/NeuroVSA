package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPackWireJSONSurvivesJavaScript: site/vocab_seed must be QUOTED strings on the wire —
// a bare JSON number above 2^53 is silently corrupted by JavaScript's JSON round-trip,
// which would invalidate signatures in transit (the bug that live sync exposed).
func TestPackWireJSONSurvivesJavaScript(t *testing.T) {
	const bigSite = uint64(6726813379716463531) // > 2^53, a real creatureSite value
	p := Pack{Name: "wire", VocabSeed: 42, Site: bigSite}

	blob, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"site":"6726813379716463531"`) {
		t.Fatalf("site must be a quoted string on the wire, got: %s", blob)
	}

	var back Pack
	if err := json.Unmarshal(blob, &back); err != nil {
		t.Fatal(err)
	}
	if back.Site != bigSite || back.VocabSeed != 42 {
		t.Fatalf("round-trip lost precision: %+v", back)
	}

	// Back-compat: bare numbers (pre-string writers) still parse.
	var legacy Pack
	if err := json.Unmarshal([]byte(`{"name":"old","vocab_seed":7,"site":99,"entries":[]}`), &legacy); err != nil {
		t.Fatalf("legacy numeric fields must still parse: %v", err)
	}
	if legacy.Site != 99 || legacy.VocabSeed != 7 {
		t.Fatalf("legacy parse wrong: %+v", legacy)
	}
}
