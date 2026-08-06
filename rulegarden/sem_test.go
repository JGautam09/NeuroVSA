package rulegarden

import (
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/engine"
)

// The structural P1-4 contract: lesson meaning lives in the sem field — written by Teach,
// read by Transfer, validated against the bound vector at import — and the display label
// carries no authority.

func TestTeachWritesSem(t *testing.T) {
	b := NewBrain(NewVocab())
	p := PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "E"}
	id, err := b.Teach(p, ActMoveAway)
	if err != nil {
		t.Fatal(err)
	}
	rec := findRecord(t, b, id)
	ls, err := ParseLessonSem(rec.Sem)
	if err != nil {
		t.Fatalf("taught lesson has unparseable sem %q: %v", rec.Sem, err)
	}
	if ls.Percept != p || ls.Action != ActMoveAway || ls.Parent != "" {
		t.Fatalf("sem does not record the taught lesson: %+v", ls)
	}
}

// TestTransferReadsSemNotLabel: Transfer must derive the new lesson from the sem record.
// The proof: a lesson imported with a valid sem but a FREE-TEXT label (which the old
// label parser cannot read) transfers fine — and the transferred lesson's sem names its
// parent as structured lineage.
func TestTransferReadsSemNotLabel(t *testing.T) {
	src := NewWorld(201)
	if err := src.Apply(Event{Op: "spawn_creature", X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	p := PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "E"}
	if err := src.Apply(Event{Op: "teach", Creature: 1, Percept: &p, Action: ActMoveAway}); err != nil {
		t.Fatal(err)
	}
	packs, err := src.BrainPacks("free-label", 1)
	if err != nil {
		t.Fatal(err)
	}
	packs[0].Entries[0].Label = "a label no parser could love"

	dst := NewWorld(202)
	if err := dst.Apply(Event{Op: "spawn_creature", X: 2, Y: 2}); err != nil {
		t.Fatal(err)
	}
	if err := dst.ApplyLessonPackTo(packs[0], 1); err != nil {
		t.Fatalf("free-text label with valid sem must import: %v", err)
	}

	brain := dst.Creatures[0].Brain
	imported := brain.Lessons()[0]
	newID, err := brain.Transfer(imported.ID, "guard")
	if err != nil {
		t.Fatalf("transfer from a sem-carrying lesson must work regardless of label: %v", err)
	}
	got := findRecord(t, brain, newID)
	ls, err := ParseLessonSem(got.Sem)
	if err != nil {
		t.Fatal(err)
	}
	want := PerceptSpec{Sees: "guard", Dist: DistNear, Dir: "E"}
	if ls.Percept != want || ls.Action != ActMoveAway {
		t.Fatalf("transferred sem is %+v, want %s → %s", ls, want, ActMoveAway)
	}
	if ls.Parent != imported.ID.String() {
		t.Fatalf("transferred sem parent is %q, want %q", ls.Parent, imported.ID)
	}
}

// TestTransferRefusesLegacyLesson: a lesson imported WITHOUT a sem (legacy pre-0.9 pack)
// must refuse to transfer — its label was validated at import, but meaning-from-label is
// exactly the mechanism the P1-4 fix retired.
func TestTransferRefusesLegacyLesson(t *testing.T) {
	src := NewWorld(203)
	if err := src.Apply(Event{Op: "spawn_creature", X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	p := PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "E"}
	if err := src.Apply(Event{Op: "teach", Creature: 1, Percept: &p, Action: ActMoveAway}); err != nil {
		t.Fatal(err)
	}
	packs, err := src.BrainPacks("legacy", 1)
	if err != nil {
		t.Fatal(err)
	}
	packs[0].Entries[0].Sem = "" // what a v0.8.x peer would send

	dst := NewWorld(204)
	if err := dst.Apply(Event{Op: "spawn_creature", X: 2, Y: 2}); err != nil {
		t.Fatal(err)
	}
	if err := dst.ApplyLessonPackTo(packs[0], 1); err != nil {
		t.Fatalf("legacy pack with a consistent label must still import: %v", err)
	}
	brain := dst.Creatures[0].Brain
	legacy := brain.Lessons()[0]
	if legacy.Sem != "" {
		t.Fatalf("legacy import grew a sem from nowhere: %q", legacy.Sem)
	}
	_, err = brain.Transfer(legacy.ID, "guard")
	if err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("transfer from a legacy lesson must refuse with a clear error, got %v", err)
	}
}

// TestParseLessonSemStrict: the sem parser is a trust-boundary parser — versions, domains,
// unknown fields, trailing data, invalid percepts, unknown actions, malformed parents all
// refuse.
func TestParseLessonSemStrict(t *testing.T) {
	good := EncodeLessonSem(PerceptSpec{Sees: "food", Dist: DistFar, Dir: "W"}, ActEat, "3:9")
	if _, err := ParseLessonSem(good); err != nil {
		t.Fatalf("canonical sem must parse: %v", err)
	}
	bad := []string{
		``,
		`not json`,
		`{"v":2,"domain":"rulegarden","percept":{"sees":"nothing"},"action":"wander"}`,
		`{"v":1,"domain":"other","percept":{"sees":"nothing"},"action":"wander"}`,
		`{"v":1,"domain":"rulegarden","percept":{"sees":"dragon","dist":"near","dir":"N"},"action":"eat"}`,
		`{"v":1,"domain":"rulegarden","percept":{"sees":"nothing"},"action":"self-destruct"}`,
		`{"v":1,"domain":"rulegarden","percept":{"sees":"nothing"},"action":"wander","parent":"not-an-id"}`,
		`{"v":1,"domain":"rulegarden","percept":{"sees":"nothing"},"action":"wander","extra":true}`,
		`{"v":1,"domain":"rulegarden","percept":{"sees":"nothing"},"action":"wander"} trailing`,
	}
	for _, s := range bad {
		if _, err := ParseLessonSem(s); err == nil {
			t.Errorf("sem %q must be refused", s)
		}
	}
}

// TestReplayRegeneratesSem: replaying a world pack re-runs teach/transfer, so replayed
// brains carry identical sems and the world hash still matches — sem is deterministic
// derived state, not an out-of-band annotation.
func TestReplayRegeneratesSem(t *testing.T) {
	w := NewWorld(205)
	if err := w.Apply(Event{Op: "spawn_creature", X: 3, Y: 3}); err != nil {
		t.Fatal(err)
	}
	p := PerceptSpec{Sees: "food", Dist: DistNear, Dir: "N"}
	if err := w.Apply(Event{Op: "teach", Creature: 1, Percept: &p, Action: ActEat}); err != nil {
		t.Fatal(err)
	}
	lesson := w.Creatures[0].Brain.Lessons()[0]
	if err := w.Apply(Event{Op: "transfer", Creature: 1, Lesson: &lesson.ID, NewSees: "water"}); err != nil {
		t.Fatal(err)
	}
	w.Step()

	replayed, err := Replay(w.Export())
	if err != nil {
		t.Fatal(err)
	}
	if w.Hash() != replayed.Hash() {
		t.Fatal("replay diverged with sem in the hash")
	}
	orig, back := w.Creatures[0].Brain.Lessons(), replayed.Creatures[0].Brain.Lessons()
	for i := range orig {
		if orig[i].Sem != back[i].Sem {
			t.Fatalf("replayed sem diverged at %d:\n %q\n %q", i, orig[i].Sem, back[i].Sem)
		}
	}
}

// TestProvenanceOf: the display-provenance helper the wasm bridge uses — structured for
// sem lessons, unstructured for legacy or malformed sems.
func TestProvenanceOf(t *testing.T) {
	pr := ProvenanceOf(engine.AssociationRecord{Sem: EncodeLessonSem(PerceptSpec{Sees: "food", Dist: DistNear, Dir: "N"}, ActEat, "1:1")})
	if !pr.Structured || pr.Percept != "sees:food,near,N" || pr.Action != ActEat || pr.Parent != "1:1" {
		t.Fatalf("unexpected provenance: %+v", pr)
	}
	if ProvenanceOf(engine.AssociationRecord{Label: "sees:food,near,N → eat"}).Structured {
		t.Fatal("legacy record must not present as structured")
	}
	if ProvenanceOf(engine.AssociationRecord{Sem: "garbage"}).Structured {
		t.Fatal("malformed sem must not present as structured")
	}
}

func findRecord(t *testing.T, b *Brain, id engine.AssociationID) engine.AssociationRecord {
	t.Helper()
	for _, rec := range b.Lessons() {
		if rec.ID == id {
			return rec
		}
	}
	t.Fatalf("record %s not found", id)
	return engine.AssociationRecord{}
}
