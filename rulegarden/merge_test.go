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
	if cert.Chosen != "action:"+ActMoveAway {
		t.Fatalf("receipt chose %q", cert.Chosen)
	}
	if !strings.Contains(cert.Note, "basis=lesson") || !strings.Contains(cert.Note, "acted=move-away") {
		t.Fatalf("receipt note lacks context: %q", cert.Note)
	}

	// The verifier's exact path: restore the brain from the image, re-execute the receipt.
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
	if !strings.Contains(cert2.Note, "basis=instinct") || !strings.Contains(cert2.Note, "acted=wander") {
		t.Fatalf("instinct receipt note wrong: %q", cert2.Note)
	}
	restored2 := engine.NewAssociativeMemory()
	if err := restored2.UnmarshalBinary(img2); err != nil {
		t.Fatal(err)
	}
	if res := cert2.VerifyAgainst(restored2); !res.OK() {
		t.Fatalf("instinct receipt failed verification: %+v", res)
	}
}
