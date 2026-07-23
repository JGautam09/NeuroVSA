package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// The NeuroMesh CRDT laws, proven as properties over scripted replicas. Ops are generated
// deterministically (seeded token names), replicas are rebuilt fresh for every scenario, and
// state equality is bit-exact via Fingerprint (which covers the convergent state: ledger set
// + tombstones + vocab seed, excluding writer identity).

// siteScript describes one writer's history: how many associations it stores and which of
// its own (1-based) sequence numbers it later removes.
type siteScript struct {
	site    uint64
	stores  int
	removes []uint64
}

// buildReplica constructs a fresh memory for one site and plays its script.
func buildReplica(t *testing.T, dict *core.TokenDictionary, s siteScript) *AssociativeMemory {
	t.Helper()
	m := NewAssociativeMemory()
	m.SetSite(s.site)
	for i := 0; i < s.stores; i++ {
		ctx := dict.GetOrRegister(fmt.Sprintf("s%d-ctx-%d", s.site, i))
		tgt := dict.GetOrRegister(fmt.Sprintf("s%d-tgt-%d", s.site, i))
		m.StoreLabeled(ctx, tgt, fmt.Sprintf("site%d/lesson%d", s.site, i+1))
	}
	for _, seq := range s.removes {
		if err := m.RemoveAssociation(AssociationID{Site: s.site, Seq: seq}); err != nil {
			t.Fatalf("site %d: remove seq %d: %v", s.site, seq, err)
		}
	}
	return m
}

var meshScripts = []siteScript{
	{site: 1, stores: 5, removes: []uint64{2}},
	{site: 2, stores: 4, removes: []uint64{1, 4}},
	{site: 3, stores: 6, removes: nil},
}

func mustMerge(t *testing.T, dst, src *AssociativeMemory) MergeReport {
	t.Helper()
	rep, err := dst.Merge(src)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	return rep
}

// naiveMatrix recomputes the majority vector from scratch over the ACTIVE ledger — the
// independent reference the incremental tally must match bit-for-bit after every merge.
func naiveCheck(t *testing.T, m *AssociativeMemory, dict *core.TokenDictionary) {
	t.Helper()
	var active []core.Hypervector
	for _, rec := range m.Ledger() {
		if rec.Removed {
			continue
		}
		// Reconstruct the bound vector from the deterministic label scheme.
		var site uint64
		var n int
		if _, err := fmt.Sscanf(rec.Label, "site%d/lesson%d", &site, &n); err != nil {
			t.Fatalf("unparseable test label %q", rec.Label)
		}
		ctx := dict.GetOrRegister(fmt.Sprintf("s%d-ctx-%d", site, n-1))
		tgt := dict.GetOrRegister(fmt.Sprintf("s%d-tgt-%d", site, n-1))
		active = append(active, ctx.Bind(tgt))
	}
	if got, want := m.Matrix(), core.Bundle(active); got != want {
		t.Fatalf("incremental tally diverged from naive Bundle over %d active entries: hamming=%d",
			len(active), core.HammingDistance(got, want))
	}
	if m.Total() != uint64(len(active)) {
		t.Fatalf("total %d != active entries %d", m.Total(), len(active))
	}
}

func TestMergeCommutative(t *testing.T) {
	dict := core.NewTokenDictionary()

	a1 := buildReplica(t, dict, meshScripts[0])
	b1 := buildReplica(t, dict, meshScripts[1])
	mustMerge(t, a1, b1) // A ← B

	a2 := buildReplica(t, dict, meshScripts[0])
	b2 := buildReplica(t, dict, meshScripts[1])
	mustMerge(t, b2, a2) // B ← A

	if a1.Fingerprint() != b2.Fingerprint() {
		t.Fatal("merge is not commutative: A←B and B←A diverged")
	}
	naiveCheck(t, a1, dict)
}

func TestMergeAssociative(t *testing.T) {
	dict := core.NewTokenDictionary()

	// (A ← B) ← C
	x := buildReplica(t, dict, meshScripts[0])
	mustMerge(t, x, buildReplica(t, dict, meshScripts[1]))
	mustMerge(t, x, buildReplica(t, dict, meshScripts[2]))

	// A ← (B ← C)
	y := buildReplica(t, dict, meshScripts[0])
	bc := buildReplica(t, dict, meshScripts[1])
	mustMerge(t, bc, buildReplica(t, dict, meshScripts[2]))
	mustMerge(t, y, bc)

	if x.Fingerprint() != y.Fingerprint() {
		t.Fatal("merge is not associative: (A∪B)∪C and A∪(B∪C) diverged")
	}
	naiveCheck(t, x, dict)
}

func TestMergeIdempotent(t *testing.T) {
	dict := core.NewTokenDictionary()

	a := buildReplica(t, dict, meshScripts[0])
	before := a.Fingerprint()

	// Merging an identical copy of one's own history must change nothing.
	rep := mustMerge(t, a, buildReplica(t, dict, meshScripts[0]))
	if rep.Added != 0 || rep.TombstonesApplied != 0 {
		t.Fatalf("self-history merge should add nothing, got %+v", rep)
	}
	if a.Fingerprint() != before {
		t.Fatal("merge is not idempotent: merging own history changed state")
	}

	// Merging the same peer twice must be a no-op the second time.
	b := buildReplica(t, dict, meshScripts[1])
	mustMerge(t, a, b)
	mid := a.Fingerprint()
	rep = mustMerge(t, a, b)
	if rep.Added != 0 || a.Fingerprint() != mid {
		t.Fatalf("second merge of the same peer changed state (added %d)", rep.Added)
	}

	// Literal self-merge is a guarded no-op.
	rep = mustMerge(t, a, a)
	if a.Fingerprint() != mid {
		t.Fatal("self-merge changed state")
	}
	naiveCheck(t, a, dict)
}

// Convergence: three replicas gossiping in entirely different orders — including partial
// exchanges — must reach bit-identical state once every pair has communicated.
func TestMergeConvergenceUnderInterleavings(t *testing.T) {
	dict := core.NewTokenDictionary()

	build3 := func() (*AssociativeMemory, *AssociativeMemory, *AssociativeMemory) {
		return buildReplica(t, dict, meshScripts[0]),
			buildReplica(t, dict, meshScripts[1]),
			buildReplica(t, dict, meshScripts[2])
	}
	fullSync := func(a, b, c *AssociativeMemory) {
		mustMerge(t, a, b)
		mustMerge(t, a, c)
		mustMerge(t, b, a)
		mustMerge(t, c, a)
	}

	// Order 1: ring gossip A←B, C←A, B←C, then full sync.
	a1, b1, c1 := build3()
	mustMerge(t, a1, b1)
	mustMerge(t, c1, a1)
	mustMerge(t, b1, c1)
	fullSync(a1, b1, c1)

	// Order 2: star around C, then full sync.
	a2, b2, c2 := build3()
	mustMerge(t, c2, a2)
	mustMerge(t, c2, b2)
	mustMerge(t, a2, c2)
	mustMerge(t, b2, c2)
	fullSync(a2, b2, c2)

	fps := map[string]bool{
		a1.Fingerprint(): true, b1.Fingerprint(): true, c1.Fingerprint(): true,
		a2.Fingerprint(): true, b2.Fingerprint(): true, c2.Fingerprint(): true,
	}
	if len(fps) != 1 {
		t.Fatalf("replicas did not converge: %d distinct fingerprints", len(fps))
	}
	naiveCheck(t, a1, dict)
}

// Tombstones must propagate: a lesson forgotten on one replica disappears everywhere the
// merge reaches, and the receiving matrix equals a Bundle over the surviving pairs.
func TestMergeTombstonePropagation(t *testing.T) {
	dict := core.NewTokenDictionary()

	a := buildReplica(t, dict, siteScript{site: 1, stores: 3})
	b := NewAssociativeMemory()
	b.SetSite(2)
	mustMerge(t, b, a) // b now knows site1's three lessons

	// b forgets site1's lesson 2, then a merges b back.
	if err := b.RemoveAssociation(AssociationID{Site: 1, Seq: 2}); err != nil {
		t.Fatal(err)
	}
	rep := mustMerge(t, a, b)
	if rep.TombstonesApplied != 1 {
		t.Fatalf("expected 1 tombstone applied, got %+v", rep)
	}
	if a.Total() != 2 {
		t.Fatalf("total after tombstone propagation = %d, want 2", a.Total())
	}
	for _, rec := range a.Ledger() {
		if rec.ID == (AssociationID{Site: 1, Seq: 2}) && !rec.Removed {
			t.Fatal("tombstone did not propagate")
		}
	}
	naiveCheck(t, a, dict)

	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("replicas diverged after tombstone propagation")
	}
}

func TestMergeGuards(t *testing.T) {
	dict := core.NewTokenDictionary()

	// Vocab seed mismatch.
	a := NewAssociativeMemory()
	b := NewAssociativeMemory()
	b.SetVocabSeed(99)
	if _, err := a.Merge(b); err == nil || !strings.Contains(err.Error(), "vocab seed") {
		t.Fatalf("expected vocab-seed error, got %v", err)
	}

	// Site collision: same ID, different content, no partial effects.
	x := NewAssociativeMemory()
	x.SetSite(7)
	x.StoreLabeled(dict.GetOrRegister("xa"), dict.GetOrRegister("xb"), "x's lesson")
	y := NewAssociativeMemory()
	y.SetSite(7) // misuse: same site, different writer
	y.StoreLabeled(dict.GetOrRegister("ya"), dict.GetOrRegister("yb"), "y's lesson")
	before := x.Fingerprint()
	if _, err := x.Merge(y); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected site-collision error, got %v", err)
	}
	if x.Fingerprint() != before {
		t.Fatal("failed merge left partial effects")
	}

	// Read-only source refuses (its ledger is not loaded).
	pathMem := buildReplica(t, dict, siteScript{site: 5, stores: 1})
	tmp := t.TempDir() + "/ro.bin"
	if err := pathMem.SaveToFile(tmp); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAssociativeMemory().Merge(ro); err == nil {
		t.Fatal("expected error merging from a read-only memory")
	}
	if _, err := ro.Merge(NewAssociativeMemory()); err == nil {
		t.Fatal("expected error merging into a read-only memory")
	}
}

// Sequence safety: after merging back our own exported history, new stores must not collide
// with the merged-in IDs.
func TestMergeOwnSiteSequenceAdvance(t *testing.T) {
	dict := core.NewTokenDictionary()

	orig := buildReplica(t, dict, siteScript{site: 9, stores: 3})
	fresh := NewAssociativeMemory()
	fresh.SetSite(9) // same writer identity, empty history (e.g. reinstalled device)
	mustMerge(t, fresh, orig)

	id := fresh.StoreLabeled(dict.GetOrRegister("new-ctx"), dict.GetOrRegister("new-tgt"), "post-merge lesson")
	if id.Seq <= 3 {
		t.Fatalf("post-merge store reused a merged sequence number: %s", id)
	}
}

// The v3 round-trip must preserve mergeability: save two replicas, load them, merge the
// loaded copies, and match the in-memory merge bit-for-bit.
func TestMergeSurvivesPersistence(t *testing.T) {
	dict := core.NewTokenDictionary()
	dir := t.TempDir()

	a := buildReplica(t, dict, meshScripts[0])
	b := buildReplica(t, dict, meshScripts[1])

	inMem := buildReplica(t, dict, meshScripts[0])
	mustMerge(t, inMem, buildReplica(t, dict, meshScripts[1]))

	if err := a.SaveToFile(dir + "/a.bin"); err != nil {
		t.Fatal(err)
	}
	if err := b.SaveToFile(dir + "/b.bin"); err != nil {
		t.Fatal(err)
	}
	la, lb := NewAssociativeMemory(), NewAssociativeMemory()
	if err := la.LoadFromFile(dir + "/a.bin"); err != nil {
		t.Fatal(err)
	}
	if err := lb.LoadFromFile(dir + "/b.bin"); err != nil {
		t.Fatal(err)
	}
	mustMerge(t, la, lb)

	if la.Fingerprint() != inMem.Fingerprint() {
		t.Fatal("merge of persisted replicas diverged from in-memory merge")
	}
}
