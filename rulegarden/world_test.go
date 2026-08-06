package rulegarden

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
)

// lessonID builds the id pointer for a creature's Nth lesson — brains claim deterministic
// per-world sites, so ids are (creatureSite(seed, creature), seq).
func lessonID(w *World, creatureID int, seq uint64) *engine.AssociationID {
	return &engine.AssociationID{Site: creatureSite(w.Seed, creatureID), Seq: seq}
}

// buildDemoWorld scripts a small deterministic scenario used across tests: one creature, one
// predator, one food, two taught lessons, a transfer, and a forget, over 50 ticks.
func buildDemoWorld(t *testing.T) *World {
	t.Helper()
	w := NewWorld(42)
	mustApply := func(e Event) {
		t.Helper()
		if err := w.Apply(e); err != nil {
			t.Fatalf("apply %+v: %v", e, err)
		}
	}

	mustApply(Event{Op: "spawn_creature", X: 12, Y: 12})
	mustApply(Event{Op: "spawn_object", Kind: KindPredator, X: 20, Y: 12})
	mustApply(Event{Op: "spawn_object", Kind: KindFood, X: 4, Y: 4})

	for w.Tick < 5 {
		w.Step()
	}
	mustApply(Event{Op: "teach", Creature: 1, Percept: &PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "E"}, Action: ActMoveAway})
	mustApply(Event{Op: "teach", Creature: 1, Percept: &PerceptSpec{Sees: "food", Dist: DistNear, Dir: "W"}, Action: ActEat})

	for w.Tick < 10 {
		w.Step()
	}
	mustApply(Event{Op: "transfer", Creature: 1, Lesson: lessonID(w, 1, 1), NewSees: "guard"})

	for w.Tick < 15 {
		w.Step()
	}
	mustApply(Event{Op: "forget", Creature: 1, Lesson: lessonID(w, 1, 2)})

	for w.Tick < 50 {
		w.Step()
	}
	return w
}

// The replay guarantee: same pack ⇒ bit-identical world hash — pinned by a committed golden
// hash so CI on ubuntu AND macos proves cross-platform determinism, exactly like the core
// golden vectors. Regenerate deliberately with UPDATE_GOLDEN=1.
func TestWorldReplayDeterminism(t *testing.T) {
	w := buildDemoWorld(t)
	hash := w.Hash()

	// In-process replay from the exported pack must reproduce the hash.
	replayed, err := Replay(w.Export())
	if err != nil {
		t.Fatal(err)
	}
	if got := replayed.Hash(); got != hash {
		t.Fatalf("replayed hash differs:\n live   %s\n replay %s", hash, got)
	}

	// Cross-platform pin — only meaningful at the default dimension (the replay-vs-live
	// equality above still ran, so determinism itself is checked on every build).
	if core.Dimension != 10000 {
		t.Skip("golden world hash is pinned at the default dimension")
	}
	golden := filepath.Join("testdata", "replay_golden.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(hash+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("regenerated %s", golden)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden hash missing (generate with UPDATE_GOLDEN=1): %v", err)
	}
	if strings.TrimSpace(string(want)) != hash {
		t.Fatalf("world hash diverged from golden:\n golden %s\n got    %s", strings.TrimSpace(string(want)), hash)
	}
}

func TestPackJSONRoundTrip(t *testing.T) {
	w := buildDemoWorld(t)
	data, err := w.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ImportJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Hash() != w.Hash() {
		t.Fatal("JSON pack round-trip changed the world hash")
	}
}

// Teaching must change behavior: an untaught creature wanders on instinct; after one taught
// lesson it flees the predator, and the decision names the lesson that caused it.
func TestTeachChangesBehaviorAndTracesCause(t *testing.T) {
	w := NewWorld(7)
	if err := w.Apply(Event{Op: "spawn_creature", X: 10, Y: 10}); err != nil {
		t.Fatal(err)
	}
	if err := w.Apply(Event{Op: "spawn_object", Kind: KindPredator, X: 13, Y: 10}); err != nil {
		t.Fatal(err)
	}
	c := w.Creatures[0]

	// Untaught: instinct.
	w.Step()
	if c.LastDecision.Basis != "instinct" || c.LastDecision.Action != ActWander {
		t.Fatalf("untaught creature should wander on instinct, got %+v", c.LastDecision)
	}

	// Teach flee-predator (near, any direction bucket the world will produce is E here).
	if err := w.Apply(Event{Op: "teach", Creature: 1, Percept: &PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "E"}, Action: ActMoveAway}); err != nil {
		t.Fatal(err)
	}

	// Reposition determinism note: predator wanders too, so assert on the DECISION whenever
	// the taught percept recurs, not on absolute positions.
	fled := false
	for i := 0; i < 30 && !fled; i++ {
		w.Step()
		d := c.LastDecision
		if d.Percept.Sees == "predator" && d.Percept.Dist == DistNear && d.Percept.Dir == "E" {
			if d.Action != ActMoveAway || d.Basis != "lesson" {
				t.Fatalf("taught percept produced %+v, want move-away via lesson", d)
			}
			if len(d.Contributors) != 1 || !strings.Contains(d.Contributors[0].Label, "predator,near,E → move-away") {
				t.Fatalf("decision did not name the causing lesson: %+v", d.Contributors)
			}
			if d.Margin < MinDecisionMargin {
				t.Fatalf("taught recall margin %d below MinDecisionMargin %d", d.Margin, MinDecisionMargin)
			}
			fled = true
		}
	}
	if !fled {
		t.Fatal("taught percept never recurred within 30 ticks (scenario needs adjusting)")
	}
}

// Forgetting must restore instinct for exactly that percept.
func TestForgetRestoresInstinct(t *testing.T) {
	w := NewWorld(9)
	if err := w.Apply(Event{Op: "spawn_creature", X: 5, Y: 5}); err != nil {
		t.Fatal(err)
	}
	c := w.Creatures[0]
	p := PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "N"}

	if err := w.Apply(Event{Op: "teach", Creature: 1, Percept: &p, Action: ActMoveAway}); err != nil {
		t.Fatal(err)
	}
	if d := c.Brain.Decide(p); d.Action != ActMoveAway || d.Basis != "lesson" {
		t.Fatalf("after teach: %+v", d)
	}
	if err := w.Apply(Event{Op: "forget", Creature: 1, Lesson: lessonID(w, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if d := c.Brain.Decide(p); d.Action != ActWander || d.Basis != "instinct" {
		t.Fatalf("after forget, expected instinct wander, got %+v", d)
	}
}

// The analogy verb: transfer flee-predator to guard; the new lesson fires for guard percepts
// and its label records the lineage. The original must be unaffected by later forgetting of
// the transferred copy (no cascade).
func TestTransferAnalogy(t *testing.T) {
	v := NewVocab()
	b := NewBrain(v)

	orig, err := b.Teach(PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "N"}, ActMoveAway)
	if err != nil {
		t.Fatal(err)
	}
	derived, err := b.Transfer(orig, "guard")
	if err != nil {
		t.Fatal(err)
	}

	d := b.Decide(PerceptSpec{Sees: "guard", Dist: DistNear, Dir: "N"})
	if d.Action != ActMoveAway || d.Basis != "lesson" {
		t.Fatalf("guard percept after transfer: %+v", d)
	}
	if len(d.Contributors) != 1 || d.Contributors[0].ID != derived {
		t.Fatalf("guard decision should name the derived lesson %d, got %+v", derived, d.Contributors)
	}
	if !strings.Contains(d.Contributors[0].Label, "from lesson 0:1") {
		t.Fatalf("derived lesson label lacks lineage: %q", d.Contributors[0].Label)
	}

	// No cascade: forgetting the derived lesson leaves the original working.
	if err := b.Forget(derived); err != nil {
		t.Fatal(err)
	}
	if d := b.Decide(PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "N"}); d.Action != ActMoveAway || d.Basis != "lesson" {
		t.Fatalf("original lesson broken by forgetting the derived one: %+v", d)
	}

	// With the derived lesson gone, guard(near,N) shares 2 of 3 roles with the predator
	// lesson — real VSA soft matching. The glass-box must call this what it is: a
	// GENERALIZATION (no exact lesson) that names its analogical source.
	d = b.Decide(PerceptSpec{Sees: "guard", Dist: DistNear, Dir: "N"})
	if d.Basis != "generalization" || d.Action != ActMoveAway {
		t.Fatalf("guard after forgetting the transfer should generalize from the predator lesson, got %+v", d)
	}
	if len(d.Contributors) != 1 || d.Contributors[0].ID != orig {
		t.Fatalf("generalization should name lesson %d as its source, got %+v", orig, d.Contributors)
	}

	// A percept sharing NO roles with any lesson must still be instinct, not generalization.
	if d := b.Decide(PerceptSpec{Sees: "water", Dist: DistFar, Dir: "S"}); d.Basis != "instinct" {
		t.Fatalf("unrelated percept should be instinct, got %+v", d)
	}
}

// Untaught percepts must fall back to instinct even when OTHER lessons exist — the margin
// rule separating recall from noise, validated empirically (G0 predicted ≥450 vs ≪200 bits).
func TestInstinctFallbackOnUntaughtPercept(t *testing.T) {
	v := NewVocab()
	b := NewBrain(v)
	if _, err := b.Teach(PerceptSpec{Sees: "predator", Dist: DistNear, Dir: "N"}, ActMoveAway); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Teach(PerceptSpec{Sees: "food", Dist: DistNear, Dir: "S"}, ActEat); err != nil {
		t.Fatal(err)
	}

	d := b.Decide(PerceptSpec{Sees: "water", Dist: DistFar, Dir: "W"})
	if d.Basis != "instinct" || d.Action != ActWander {
		t.Fatalf("untaught percept should be instinct wander, got %+v (margin %d)", d, d.Margin)
	}
	if d.Margin >= MinDecisionMargin {
		t.Fatalf("untaught percept produced a suspiciously large margin: %d", d.Margin)
	}
}
