package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// jsCycle simulates a JavaScript JSON.parse → JSON.stringify hop: decoding into
// interface{} turns every JSON number into a float64 (JS's only number type), exactly as a
// browser does. A bare 64-bit integer above 2^53 is silently corrupted here; a quoted
// decimal string survives. This is the "actual parse/stringify cycle" the review asked for,
// reproduced in Go so it runs in CI without a browser.
func jsCycle(t *testing.T, blob []byte) []byte {
	t.Helper()
	var generic any
	if err := json.Unmarshal(blob, &generic); err != nil {
		t.Fatalf("js parse: %v", err)
	}
	out, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("js stringify: %v", err)
	}
	return out
}

// TestPackSurvivesJSCycle: a pack with 64-bit site AND entry seq above 2^53 must survive a
// simulated browser JSON round-trip unchanged — signatures included.
func TestPackSurvivesJSCycle(t *testing.T) {
	const bigSite = uint64(6726813379716463531) // > 2^53
	const bigSeq = uint64(9007199254740993)     // 2^53 + 1, the first unrepresentable odd int
	p := Pack{Name: "wire", VocabSeed: 18446744073709551557, Site: bigSite,
		Entries: []PackEntry{{Seq: bigSeq, Label: "l"}}}

	blob, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"site":"6726813379716463531"`, `"seq":"9007199254740993"`, `"vocab_seed":"18446744073709551557"`} {
		if !strings.Contains(string(blob), want) {
			t.Fatalf("expected quoted %s in wire JSON, got: %s", want, blob)
		}
	}

	var back Pack
	if err := json.Unmarshal(jsCycle(t, blob), &back); err != nil {
		t.Fatal(err)
	}
	if back.Site != bigSite || back.VocabSeed != p.VocabSeed || back.Entries[0].Seq != bigSeq {
		t.Fatalf("JS cycle corrupted a 64-bit field: site=%d seed=%d seq=%d", back.Site, back.VocabSeed, back.Entries[0].Seq)
	}

	// Back-compat: pre-string writers emitted bare numbers; those still parse.
	zeroHex := strings.Repeat("0", core.NumWords*16) // a valid all-zeros bound vector
	var legacy Pack
	if err := json.Unmarshal([]byte(`{"name":"old","vocab_seed":7,"site":99,"entries":[{"seq":3,"label":"l","bound_hex":"`+zeroHex+`"}]}`), &legacy); err != nil {
		t.Fatalf("legacy numeric fields must still parse: %v", err)
	}
	if legacy.Site != 99 || legacy.VocabSeed != 7 || legacy.Entries[0].Seq != 3 {
		t.Fatalf("legacy parse wrong: %+v", legacy)
	}
}

// TestCertificateVocabSeedSurvivesJSCycle: the receipt's vocab seed is signed-over, so a JS
// hop that corrupted it would break verification. It must be quoted.
func TestCertificateVocabSeedSurvivesJSCycle(t *testing.T) {
	c := DecisionCertificate{EngineVersion: Version, VocabSeed: 18446744073709551557, MemoryFingerprint: "fp",
		CandidateTokens: []string{"a"}, Chosen: "a", Candidates: []core.Candidate{{Token: "a", Distance: 1}}}
	blob, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"vocab_seed":"18446744073709551557"`) {
		t.Fatalf("cert vocab_seed must be quoted, got: %s", blob)
	}
	var back DecisionCertificate
	if err := json.Unmarshal(jsCycle(t, blob), &back); err != nil {
		t.Fatal(err)
	}
	if back.VocabSeed != c.VocabSeed {
		t.Fatalf("JS cycle corrupted cert vocab seed: %d != %d", back.VocabSeed, c.VocabSeed)
	}
}
