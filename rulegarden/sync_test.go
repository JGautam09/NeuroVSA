package rulegarden

import (
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/engine"
)

// syncWorld builds a world with one creature, used as a live-sync peer.
func syncWorld(t *testing.T, seed uint64) *World {
	t.Helper()
	w := NewWorld(seed)
	if err := w.Apply(Event{Op: "spawn_creature", X: 10, Y: 10}); err != nil {
		t.Fatal(err)
	}
	return w
}

func teach(t *testing.T, w *World, sees, dist, dir, action string) {
	t.Helper()
	if err := w.Apply(Event{Op: "teach", Creature: 1, Percept: &PerceptSpec{Sees: sees, Dist: dist, Dir: dir}, Action: action}); err != nil {
		t.Fatal(err)
	}
}

// send simulates the live-sync wire: export the sender's brain (one pack per author site),
// apply each at the receiver.
func send(t *testing.T, from, to *World) {
	t.Helper()
	packs, err := from.BrainPacks("sync", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range packs {
		if err := to.ApplyLessonPackTo(p, 1); err != nil {
			t.Fatal(err)
		}
	}
}

func fingerprint(t *testing.T, w *World) string {
	t.Helper()
	return w.Creatures[0].Brain.Memory().Fingerprint()
}

// TestLiveSyncConvergence is the Phase C core claim: two peers exchanging flat brain packs
// converge to bit-identical brains, lessons keep their author's site, and a forget
// propagates as a revocation while connected.
func TestLiveSyncConvergence(t *testing.T) {
	a, b := syncWorld(t, 71), syncWorld(t, 72)

	teach(t, a, "predator", DistNear, "E", ActMoveAway)
	teach(t, b, "food", DistNear, "N", ActEat)

	// Bidirectional sync (order arbitrary — merges commute).
	send(t, a, b)
	send(t, b, a)

	if fa, fb := fingerprint(t, a), fingerprint(t, b); fa != fb {
		t.Fatalf("brains diverge after sync: %s vs %s", fa, fb)
	}
	// Both lessons present on both sides, each under its AUTHOR's site.
	siteA, siteB := creatureSite(71, 1), creatureSite(72, 1)
	for name, w := range map[string]*World{"a": a, "b": b} {
		var sites []uint64
		for _, rec := range w.Creatures[0].Brain.Lessons() {
			if !rec.Removed {
				sites = append(sites, rec.ID.Site)
			}
		}
		if len(sites) != 2 {
			t.Fatalf("%s: want 2 active lessons, got %d", name, len(sites))
		}
		if !((sites[0] == siteA && sites[1] == siteB) || (sites[0] == siteB && sites[1] == siteA)) {
			t.Fatalf("%s: lessons lost their author sites: %v (want %d and %d)", name, sites, siteA, siteB)
		}
	}

	// A forgets ITS lesson; the revocation pack tombstones it at B too.
	var aLesson engine.AssociationID
	for _, rec := range a.Creatures[0].Brain.Lessons() {
		if rec.ID.Site == siteA && !rec.Removed {
			aLesson = rec.ID
		}
	}
	rp, err := a.RevocationPack("revoke", 1, aLesson)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(Event{Op: "forget", Creature: 1, Lesson: &aLesson}); err != nil {
		t.Fatal(err)
	}
	if err := b.RevokeLessonPackFrom(rp, 1); err != nil {
		t.Fatal(err)
	}
	if fa, fb := fingerprint(t, a), fingerprint(t, b); fa != fb {
		t.Fatalf("brains diverge after forget propagation: %s vs %s", fa, fb)
	}

	// Idempotence across reconnects: replaying the full snapshot changes nothing — and in
	// particular does NOT resurrect the tombstoned lesson.
	before := fingerprint(t, b)
	send(t, a, b)
	if fingerprint(t, b) != before {
		t.Fatal("re-sending the snapshot must be a no-op (idempotent merge)")
	}
}

// TestLiveSyncReplayDeterminism: a world whose log contains apply_pack and revoke_pack
// events still satisfies the contract — same pack, same hash.
func TestLiveSyncReplayDeterminism(t *testing.T) {
	a, b := syncWorld(t, 73), syncWorld(t, 74)
	teach(t, a, "predator", DistNear, "W", ActMoveAway)
	send(t, a, b)
	teach(t, b, "water", DistFar, "S", ActMoveToward)

	rps, err := a.BrainPacks("revoke-src", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, rp := range rps {
		if err := b.RevokeLessonPackFrom(rp, 1); err != nil { // tombstone A's lessons at B
			t.Fatal(err)
		}
	}

	pack := b.Export()
	w2, err := Replay(pack)
	if err != nil {
		t.Fatalf("replay of a sync-bearing world: %v", err)
	}
	if got, want := w2.Hash(), b.Hash(); got != want {
		t.Fatalf("replay hash %s != live hash %s", got, want)
	}
}

// TestApplyPackEventRefusesTamperedSignature: the same policy as world packs — a SIGNED
// lesson pack whose content was altered is refused, atomically.
func TestApplyPackEventRefusesTamperedSignature(t *testing.T) {
	a, b := syncWorld(t, 75), syncWorld(t, 76)
	teach(t, a, "intruder", DistNear, "N", ActMoveAway)

	ps, err := a.BrainPacks("signed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 {
		t.Fatalf("single-author brain must export one pack, got %d", len(ps))
	}
	p := ps[0]
	p.Sign(testKey(t, 9))
	if err := b.ApplyLessonPackTo(p, 1); err != nil {
		t.Fatalf("valid signed pack must apply: %v", err)
	}

	tampered := p
	tampered.Entries = append([]engine.PackEntry(nil), p.Entries...)
	tampered.Entries[0].Label = "sees:intruder,near,N → eat" // altered content, stale signature
	before := fingerprint(t, b)
	if err := b.ApplyLessonPackTo(tampered, 1); err == nil || !strings.Contains(err.Error(), "signature is invalid") {
		t.Fatalf("tampered signed pack: err = %v, want signature-invalid refusal", err)
	}
	if fingerprint(t, b) != before {
		t.Fatal("failed apply must leave the brain untouched (atomic)")
	}
}

// TestLiveSyncGossipConvergence: three peers, seeded-random teach/gossip interleaving —
// after a final full exchange round, all three brains are bit-identical. Extends the
// engine's CRDT-law property tests to the world-event sync path.
func TestLiveSyncGossipConvergence(t *testing.T) {
	worlds := []*World{syncWorld(t, 81), syncWorld(t, 82), syncWorld(t, 83)}
	rng := newRNG(4242)
	subjects := []string{"predator", "food", "guard", "prey", "water"}
	dirs := []string{"N", "S", "E", "W"}
	acts := []string{ActMoveAway, ActEat, ActMoveToward, ActWander}

	for step := 0; step < 40; step++ {
		switch rng.intn(3) {
		case 0: // someone learns something
			w := worlds[rng.intn(3)]
			teach(t, w, subjects[rng.intn(5)], []string{DistNear, DistFar}[rng.intn(2)], dirs[rng.intn(4)], acts[rng.intn(4)])
		default: // someone gossips to someone else
			i, j := rng.intn(3), rng.intn(3)
			if i != j {
				send(t, worlds[i], worlds[j])
			}
		}
	}
	// Final full mesh round so everything reaches everyone.
	for i := range worlds {
		for j := range worlds {
			if i != j {
				send(t, worlds[i], worlds[j])
			}
		}
	}
	f0 := fingerprint(t, worlds[0])
	for i, w := range worlds[1:] {
		if f := fingerprint(t, w); f != f0 {
			t.Fatalf("peer %d diverged: %s vs %s", i+1, f, f0)
		}
	}
}

// TestApplyPackRejectsForgedLabel is the P1-4 regression, covering every forgery direction
// the sem field introduces: a spoofed semantic-looking label next to a valid sem, a forged
// sem that contradicts the vector, a stripped-sem legacy downgrade with a lying label, and
// the honest original. A signature over an inconsistent (meaning, vector) pair is not
// evidence the meaning explains the vector.
func TestApplyPackRejectsForgedLabel(t *testing.T) {
	src := syncWorld(t, 91)
	teach(t, src, "predator", DistNear, "E", ActMoveAway)

	packs, err := src.BrainPacks("forge", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 || len(packs[0].Entries) != 1 {
		t.Fatalf("expected one single-entry pack, got %+v", packs)
	}

	clone := func() engine.Pack {
		p := packs[0]
		p.Entries = append([]engine.PackEntry(nil), packs[0].Entries...)
		return p
	}

	// (a) Valid sem, but a semantic-LOOKING label claiming a different lesson: the display
	// spoof. Must be refused even though sem↔vector agree.
	spoofed := clone()
	spoofed.Entries[0].Label = "sees:food,near,N → eat"
	dst := syncWorld(t, 92)
	err = dst.ApplyLessonPackTo(spoofed, 1)
	if err == nil || !strings.Contains(err.Error(), "contradicts the entry's verified semantics") {
		t.Fatalf("spoofed label next to valid sem must be refused, got err = %v", err)
	}

	// (b) Forged sem: claims food→eat while the vector encodes predator→move-away.
	forgedSem := clone()
	forgedSem.Entries[0].Sem = EncodeLessonSem(PerceptSpec{Sees: "food", Dist: DistNear, Dir: "N"}, ActEat, "")
	forgedSem.Entries[0].Label = "sees:food,near,N → eat" // label agrees with the forged sem
	err = dst.ApplyLessonPackTo(forgedSem, 1)
	if err == nil || !strings.Contains(err.Error(), "contradicts its sem") {
		t.Fatalf("forged sem/vector pair must be refused, got err = %v", err)
	}

	// (c) Legacy downgrade: strip the sem and relabel — the v0.8.1 forgery. The label-based
	// legacy rule must still catch it.
	legacyForged := clone()
	legacyForged.Entries[0].Sem = ""
	legacyForged.Entries[0].Label = "sees:food,near,N → eat"
	err = dst.ApplyLessonPackTo(legacyForged, 1)
	if err == nil || !strings.Contains(err.Error(), "contradicts its label") {
		t.Fatalf("forged legacy label/vector pair must be refused, got err = %v", err)
	}

	// And the brain is untouched (atomic refusal).
	if len(dst.Creatures[0].Brain.Lessons()) != 0 {
		t.Fatal("refused forged packs left lessons behind")
	}

	// (d) A free-text label next to a valid sem is fine — labels are display-only now.
	freeText := clone()
	freeText.Entries[0].Label = "grandma's predator wisdom"
	if err := dst.ApplyLessonPackTo(freeText, 1); err != nil {
		t.Fatalf("free-text label with valid sem must apply: %v", err)
	}

	// (e) The honest, unmodified pack still applies (idempotent with (d)'s content? no —
	// different label means different content under the same id, so a fresh world instead).
	dst2 := syncWorld(t, 93)
	if err := dst2.ApplyLessonPackTo(packs[0], 1); err != nil {
		t.Fatalf("honest pack must apply: %v", err)
	}
}
