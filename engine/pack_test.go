package engine

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// authorPack teaches lessons into a scratch memory under a claimed site and exports them.
func authorPack(t *testing.T, name string, site uint64, lessons [][2]string) Pack {
	t.Helper()
	dict := core.NewTokenDictionary()
	scratch := NewAssociativeMemory()
	scratch.SetSite(site)
	for i, l := range lessons {
		scratch.StoreLabeled(dict.GetOrRegister(l[0]), dict.GetOrRegister(l[1]), name+"/"+l[0]+"→"+l[1])
		_ = i
	}
	return PackFromMemory(name, scratch)
}

func TestPackSignJSONRoundTrip(t *testing.T) {
	p := authorPack(t, "safety-basics", 100, [][2]string{{"fire", "flee"}, {"cliff", "stop"}})

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	p.Sign(priv)
	if !p.VerifySignature() {
		t.Fatal("fresh pack signature did not verify")
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var back Pack
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !back.VerifySignature() {
		t.Fatal("signature broken by JSON round-trip")
	}
	if len(back.Entries) != 2 || back.Entries[0].Bound != p.Entries[0].Bound {
		t.Fatal("entries broken by JSON round-trip")
	}

	// Tamper: swap a label.
	back.Entries[0].Label = "evil"
	if back.VerifySignature() {
		t.Fatal("signature survived entry tampering")
	}
}

// THE composition test: two independent replicas apply the same pack, then merge — the
// pack's lessons must not double-count, and both replicas converge.
func TestPackApplyThenMergeDeduplicates(t *testing.T) {
	p := authorPack(t, "shared-pack", 200, [][2]string{{"a", "b"}, {"c", "d"}, {"e", "f"}})
	dict := core.NewTokenDictionary()

	site1 := NewAssociativeMemory()
	site1.SetSite(1)
	site1.StoreLabeled(dict.GetOrRegister("own1"), dict.GetOrRegister("t1"), "site1's own lesson")
	site2 := NewAssociativeMemory()
	site2.SetSite(2)
	site2.StoreLabeled(dict.GetOrRegister("own2"), dict.GetOrRegister("t2"), "site2's own lesson")

	if _, err := ApplyPack(site1, &p); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPack(site2, &p); err != nil {
		t.Fatal(err)
	}
	if site1.Total() != 4 || site2.Total() != 4 {
		t.Fatalf("totals after apply: %d, %d (want 4, 4)", site1.Total(), site2.Total())
	}

	// Re-applying is idempotent (the merge is).
	rep, err := ApplyPack(site1, &p)
	if err != nil || rep.Added != 0 {
		t.Fatalf("re-apply not idempotent: %+v, %v", rep, err)
	}

	// Merge the two replicas: pack entries shared (not duplicated), own lessons exchanged.
	rep, err = site1.Merge(site2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Added != 1 || rep.Shared != 3 {
		t.Fatalf("merge after shared pack: %+v (want Added=1 Shared=3)", rep)
	}
	if _, err := site2.Merge(site1); err != nil {
		t.Fatal(err)
	}
	if site1.Fingerprint() != site2.Fingerprint() {
		t.Fatal("replicas with a shared pack did not converge")
	}
	if site1.Total() != 5 { // 3 pack + 2 own
		t.Fatalf("converged total = %d, want 5", site1.Total())
	}
}

// Revocation is a unit operation and PROPAGATES: revoke on one replica, merge, gone on both.
func TestPackRevocationPropagates(t *testing.T) {
	p := authorPack(t, "revocable", 300, [][2]string{{"x", "y"}, {"p", "q"}})

	a := NewAssociativeMemory()
	a.SetSite(1)
	b := NewAssociativeMemory()
	b.SetSite(2)
	if _, err := ApplyPack(a, &p); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPack(b, &p); err != nil {
		t.Fatal(err)
	}

	if removed := RevokePack(a, &p); removed != 2 {
		t.Fatalf("revoked %d entries, want 2", removed)
	}
	if a.Total() != 0 {
		t.Fatalf("total after revoke = %d, want 0", a.Total())
	}
	// Revoking again is a no-op.
	if removed := RevokePack(a, &p); removed != 0 {
		t.Fatalf("second revoke removed %d, want 0", removed)
	}

	// The revocation travels to b through a merge; b's matrix drops the pack's votes.
	if _, err := b.Merge(a); err != nil {
		t.Fatal(err)
	}
	if b.Total() != 0 {
		t.Fatalf("revocation did not propagate: b total = %d", b.Total())
	}
	if !b.Matrix().IsZero() {
		t.Fatal("b's matrix still carries revoked votes")
	}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("replicas diverged after revocation propagation")
	}
}

func TestPackGuards(t *testing.T) {
	// Duplicate seqs are rejected at materialization.
	bad := Pack{Name: "dup", Site: 5, Entries: []PackEntry{{Seq: 1, Label: "a"}, {Seq: 1, Label: "b"}}}
	if _, err := bad.Memory(); err == nil {
		t.Fatal("expected duplicate-seq error")
	}

	// Vocab mismatch refuses to apply.
	p := authorPack(t, "vocab", 400, [][2]string{{"m", "n"}})
	target := NewAssociativeMemory()
	target.SetVocabSeed(999)
	if _, err := ApplyPack(target, &p); err == nil {
		t.Fatal("expected vocab-seed mismatch error")
	}
}
