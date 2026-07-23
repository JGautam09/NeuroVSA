package rulegarden

import (
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/engine"
)

// teachWorld builds a world with one creature and one taught lesson, returning it exported.
func teachWorld(t *testing.T, seed uint64, sees, action, dir string) *World {
	t.Helper()
	w := NewWorld(seed)
	if err := w.Apply(Event{Op: "spawn_creature", X: 5, Y: 5}); err != nil {
		t.Fatal(err)
	}
	if err := w.Apply(Event{Op: "teach", Creature: 1,
		Percept: &PerceptSpec{Sees: sees, Dist: DistNear, Dir: dir}, Action: action}); err != nil {
		t.Fatal(err)
	}
	return w
}

// Brains in different worlds must claim different sites; replay must reproduce them.
func TestCreatureSitesDistinctAndStable(t *testing.T) {
	w1 := teachWorld(t, 111, "predator", ActMoveAway, "N")
	w2 := teachWorld(t, 222, "water", ActMoveToward, "N")

	s1 := w1.Creatures[0].Brain.Memory().Site()
	s2 := w2.Creatures[0].Brain.Memory().Site()
	if s1 == s2 || s1 == 0 || s2 == 0 {
		t.Fatalf("creature sites not distinct/nonzero: %d vs %d", s1, s2)
	}

	r1, err := Replay(w1.Export())
	if err != nil {
		t.Fatal(err)
	}
	if r1.Creatures[0].Brain.Memory().Site() != s1 {
		t.Fatal("replay did not reproduce the creature's site")
	}
}

// The headline: my creature learns your creature's lessons, the foreign lesson carries its
// foreign site in my ledger, and my merged world still replays bit-exactly.
func TestMergeBrainsAcrossWorlds(t *testing.T) {
	teacher := teachWorld(t, 111, "predator", ActMoveAway, "N")
	student := teachWorld(t, 222, "food", ActEat, "N")
	sc := student.Creatures[0]

	if err := student.MergeBrainsFrom(teacher.Export(), 1); err != nil {
		t.Fatal(err)
	}

	// The student now acts on the teacher's lesson...
	d := sc.Brain.Decide(PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "N"})
	if d.Action != ActMoveAway || d.Basis != "lesson" {
		t.Fatalf("student did not learn the foreign lesson: %+v", d)
	}
	// ...and still knows its own.
	if d := sc.Brain.Decide(PerceptSpec{Sees: "food", Dist: DistNear, Dir: "N"}); d.Action != ActEat {
		t.Fatalf("student lost its own lesson: %+v", d)
	}

	// Foreign provenance is visible: the merged lesson carries the teacher's site.
	teacherSite := teacher.Creatures[0].Brain.Memory().Site()
	foundForeign := false
	for _, rec := range sc.Brain.Lessons() {
		if rec.ID.Site == teacherSite {
			foundForeign = true
		}
	}
	if !foundForeign {
		t.Fatal("merged ledger does not show the foreign site")
	}

	// Replay integrity: the merge is an event, so export → replay reproduces the hash.
	replayed, err := Replay(student.Export())
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Hash() != student.Hash() {
		t.Fatal("merged world lost replay determinism")
	}

	// And the JSON pack round-trips (nested foreign pack included).
	data, err := student.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ImportJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Hash() != student.Hash() {
		t.Fatal("merged world JSON round-trip changed the hash")
	}
}

// Same-seed worlds share writer sites; merging their divergent histories must refuse
// atomically — the student keeps its exact pre-merge state.
func TestMergeBrainsSameSeedRefusedAtomically(t *testing.T) {
	a := teachWorld(t, 333, "predator", ActMoveAway, "N")
	b := teachWorld(t, 333, "water", ActMoveToward, "N") // same seed → same creature site, different lesson

	before := a.Hash()
	err := a.MergeBrainsFrom(b.Export(), 1)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("expected a collision error, got %v", err)
	}
	if a.Hash() != before {
		t.Fatal("failed merge left partial effects (atomicity broken)")
	}
	if len(a.Events) != 2 {
		t.Fatalf("failed merge was logged: %d events", len(a.Events))
	}
}

// P2a: untrusted packs must be bounded — an absurd tick horizon, too many events, and deep
// merge nesting are all rejected before the synchronous replay loop can run away.
func TestReplayBounds(t *testing.T) {
	if _, err := Replay(Pack{Version: 1, Seed: 1, Ticks: MaxReplayTicks + 1}); err == nil {
		t.Fatal("expected a tick-horizon error")
	}
	huge := make([]Event, MaxReplayEvents+1)
	if _, err := Replay(Pack{Version: 1, Seed: 1, Ticks: 0, Events: huge}); err == nil {
		t.Fatal("expected an event-count error")
	}
	if _, err := ImportJSON(make([]byte, MaxPackBytes+1)); err == nil {
		t.Fatal("expected an oversized-pack error")
	}

	// Nested merge_brains packs beyond MaxMergeDepth must be refused. Build a chain deeper
	// than the limit.
	inner := Pack{Version: 1, Seed: 99, Ticks: 0}
	for d := 0; d < MaxMergeDepth+1; d++ {
		outer := Pack{Version: 1, Seed: uint64(d + 1), Ticks: 0, Events: []Event{
			{Tick: 0, Op: "spawn_creature", X: 1, Y: 1},
			{Tick: 0, Op: "merge_brains", Creature: 1, ForeignPack: &inner},
		}}
		inner = outer
	}
	if _, err := Replay(inner); err == nil {
		t.Fatal("expected a merge-nesting-depth error")
	}
}

// Receipts: a creature's last decision certifies and verifies against its exported brain.
func TestCertifyCreatureReceipt(t *testing.T) {
	w := teachWorld(t, 444, "predator", ActMoveAway, "E")
	if err := w.Apply(Event{Op: "spawn_object", Kind: KindPredator, X: 6, Y: 5}); err != nil {
		t.Fatal(err)
	}
	w.Step()

	cert, brainImg, err := w.CertifyCreature(1)
	if err != nil {
		t.Fatal(err)
	}
	if cert.ExecutedAction != "action:"+ActMoveAway || cert.Basis != "lesson" {
		t.Fatalf("receipt executed action/basis = %q/%q, want action:move-away/lesson", cert.ExecutedAction, cert.Basis)
	}

	// The verifier's exact path: restore the brain from the image, re-execute the receipt —
	// this now also re-derives and checks the executed action/basis.
	restored := engine.NewAssociativeMemory()
	if err := restored.UnmarshalBinary(brainImg); err != nil {
		t.Fatal(err)
	}
	if res := cert.VerifyAgainst(restored); !res.OK() {
		t.Fatalf("receipt failed verification against the exported brain: %+v", res)
	}

	// Instinct decisions are certifiable too, and say so.
	w2 := NewWorld(555)
	if err := w2.Apply(Event{Op: "spawn_creature", X: 3, Y: 3}); err != nil {
		t.Fatal(err)
	}
	w2.Step()
	cert2, img2, err := w2.CertifyCreature(1)
	if err != nil {
		t.Fatal(err)
	}
	// Instinct: the executed action is the default (wander) even though the raw cleanup
	// winner is some noise action — and it's a verifiable structured field, not a note.
	if cert2.Basis != "instinct" || cert2.ExecutedAction != "action:"+ActWander {
		t.Fatalf("instinct receipt executed/basis = %q/%q, want action:wander/instinct", cert2.ExecutedAction, cert2.Basis)
	}
	restored2 := engine.NewAssociativeMemory()
	if err := restored2.UnmarshalBinary(img2); err != nil {
		t.Fatal(err)
	}
	if res := cert2.VerifyAgainst(restored2); !res.OK() {
		t.Fatalf("instinct receipt failed verification: %+v", res)
	}

	// Tampering with the executed action must break verification (fields are signed-over and
	// re-derived).
	tampered := cert2
	tampered.ExecutedAction = "action:move-toward"
	if res := tampered.VerifyAgainst(restored2); res.OK() {
		t.Fatal("tampered executed action passed verification")
	}

	// Late-certification guard: teaching AFTER the decision must not change what the retained
	// receipt certifies (it re-executes against the decision-time image).
	if err := w.Apply(Event{Op: "teach", Creature: 1,
		Percept: &PerceptSpec{Sees: "food", Dist: DistNear, Dir: "W"}, Action: ActEat}); err != nil {
		t.Fatal(err)
	}
	certAfter, imgAfter, err := w.CertifyCreature(1)
	if err != nil {
		t.Fatal(err)
	}
	if certAfter.MemoryFingerprint != cert.MemoryFingerprint {
		t.Fatal("certificate drifted after post-decision teaching (should reflect the decision-time memory)")
	}
	restoredAfter := engine.NewAssociativeMemory()
	if err := restoredAfter.UnmarshalBinary(imgAfter); err != nil {
		t.Fatal(err)
	}
	if res := certAfter.VerifyAgainst(restoredAfter); !res.OK() {
		t.Fatalf("post-teach receipt failed verification: %+v", res)
	}
}
