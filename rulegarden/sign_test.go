package rulegarden

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// testKey returns a deterministic ed25519 key: seed = the byte pattern i, i+1, ...
func testKey(t *testing.T, first byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = first + byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// signedWorldPack builds a small taught world and returns its signed export.
func signedWorldPack(t *testing.T, seed uint64, priv ed25519.PrivateKey) Pack {
	t.Helper()
	w := NewWorld(seed)
	mustApply := func(e Event) {
		t.Helper()
		if err := w.Apply(e); err != nil {
			t.Fatalf("apply %s: %v", e.Op, err)
		}
	}
	mustApply(Event{Op: "spawn_creature", X: 12, Y: 12})
	mustApply(Event{Op: "spawn_object", Kind: KindPredator, X: 20, Y: 12})
	mustApply(Event{Op: "teach", Creature: 1, Percept: &PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "E"}, Action: ActMoveAway})
	p := w.Export()
	p.Sign(priv)
	return p
}

func TestSignedPackRoundTrip(t *testing.T) {
	priv := testKey(t, 1)
	p := signedWorldPack(t, 11, priv)

	if !p.VerifySignature() {
		t.Fatal("freshly signed pack must verify")
	}
	if got := p.SignerFingerprint(); len(got) != 16 {
		t.Fatalf("fingerprint = %q, want 16 hex chars", got)
	}

	// A signed pack must survive the JSON wire format and import (replay) cleanly.
	wire, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	w, err := ImportJSON(wire)
	if err != nil {
		t.Fatalf("import signed pack: %v", err)
	}
	if len(w.Creatures) != 1 {
		t.Fatalf("replayed world has %d creatures, want 1", len(w.Creatures))
	}

	// Exporting the imported world yields an UNSIGNED pack: the signature belonged to the
	// original author's exact content, not to whatever happens next in this world.
	if re := w.Export(); len(re.Signature) != 0 || len(re.PublicKey) != 0 {
		t.Fatal("re-export must not inherit the original signature")
	}
}

func TestTamperedPackRefused(t *testing.T) {
	p := signedWorldPack(t, 12, testKey(t, 2))

	tampered := p
	tampered.Events = append([]Event(nil), p.Events...)
	tampered.Events[2].Action = ActEat // flip the taught action: move-away -> eat

	if tampered.VerifySignature() {
		t.Fatal("tampered pack must not verify")
	}
	wire, _ := json.Marshal(tampered)
	if _, err := ImportJSON(wire); err == nil || !strings.Contains(err.Error(), "signature is invalid") {
		t.Fatalf("import of tampered signed pack: err = %v, want signature-invalid refusal", err)
	}

	// Same content unsigned imports fine — refusal is about tamper evidence, not signing policy.
	unsigned := tampered
	unsigned.PublicKey, unsigned.Signature = nil, nil
	wire, _ = json.Marshal(unsigned)
	if _, err := ImportJSON(wire); err != nil {
		t.Fatalf("unsigned variant should import, got %v", err)
	}
}

// A signed pack quoted inside another world's merge_brains event is re-verified on every
// replay of the quoting world — tampering with the quote is caught too.
func TestNestedQuotedPackTamperRefused(t *testing.T) {
	foreign := signedWorldPack(t, 21, testKey(t, 3))

	w := NewWorld(22)
	if err := w.Apply(Event{Op: "spawn_creature", X: 5, Y: 5}); err != nil {
		t.Fatal(err)
	}
	if err := w.MergeBrainsFrom(foreign, 1); err != nil {
		t.Fatalf("merge signed foreign pack: %v", err)
	}

	outer := w.Export()
	wire, err := json.Marshal(outer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportJSON(wire); err != nil {
		t.Fatalf("untampered quoting world must replay, got %v", err)
	}

	// Tamper with the QUOTED pack inside the outer world's event log.
	var mutated Pack
	if err := json.Unmarshal(wire, &mutated); err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range mutated.Events {
		if fp := mutated.Events[i].ForeignPack; fp != nil {
			fp.Events[2].Action = ActWander
			found = true
		}
	}
	if !found {
		t.Fatal("no merge_brains event with a foreign pack found")
	}
	wire2, _ := json.Marshal(mutated)
	if _, err := ImportJSON(wire2); err == nil || !strings.Contains(err.Error(), "signature is invalid") {
		t.Fatalf("replay of world quoting a tampered signed pack: err = %v, want signature-invalid refusal", err)
	}
}

// goldenPackSignature pins the exact signature bytes for a fixed key + fixed pack. ed25519
// is deterministic and CanonicalBytes is a fixed JSON encoding, so this must be bit-identical
// on every platform, every run, and in both native and wasm builds — the portability claim
// browser/CLI signature parity rests on. CI enforces it on ubuntu and macos.
const goldenPackSignature = "391d055a48ad5e067c2b06cb4c2ffb4fc36fb3e7854bd5f920f63d3638190f2b156e7c7fd9537a48f7204d625e6bd402c58de95f67c6f12696a13c6223aa1108"

func TestGoldenPackSignature(t *testing.T) {
	priv := testKey(t, 42)
	p := Pack{Version: 1, Seed: 7, Ticks: 0, Events: []Event{
		{Tick: 0, Op: "spawn_creature", X: 3, Y: 4},
		{Tick: 0, Op: "teach", Creature: 1, Percept: &PerceptSpec{Sees: "food", Dist: DistNear, Dir: "N"}, Action: ActEat},
	}}
	p.Sign(priv)
	if !p.VerifySignature() {
		t.Fatal("golden pack must verify")
	}
	got := hex.EncodeToString(p.Signature)
	if got != goldenPackSignature {
		t.Fatalf("golden signature drift:\n got  %s\n want %s\n(canonical bytes: %s)",
			got, goldenPackSignature, p.CanonicalBytes())
	}
	// The canonical bytes must exclude the signature fields (signing twice is stable).
	before := append([]byte(nil), p.CanonicalBytes()...)
	p.Sign(priv)
	if !bytes.Equal(before, p.CanonicalBytes()) {
		t.Fatal("CanonicalBytes must be independent of the signature fields")
	}
}
