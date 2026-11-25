package rulegarden

import (
	"fmt"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
)

// MinDecisionMargin is the winner-vs-runner-up Hamming gap (bits) below which a decision is
// treated as instinct (untrained) rather than a recalled lesson. G0 data at the default
// 10,000-bit dimension: taught percepts recall with margins ≥ ~450 bits (random contexts,
// K ≤ 128) and ~840 bits (structured percepts), while untaught percepts produce only
// order-statistic noise gaps (≪ 200 bits). Dimension/50 = 200 at the default — the
// empirically gated value, unchanged — and scales linearly for hd_d* study builds
// (recall margins scale ~linearly with D; the dimensionality study re-measures them).
const MinDecisionMargin = core.Dimension / 50

// GeneralizationRadius bounds the ledger scan that names the SOURCE of an analogical
// generalization. At the default dimension: exact recalls sit at probe distance 0; percepts
// sharing 2 of 3 roles with a taught lesson land near ~2500 (each bundle bit is a 3-way
// majority sharing 2 inputs, so ~75% of bits agree — measured in TestTransferAnalogy);
// unrelated lessons sit at ~5000. Dimension·33/100 = 3300 at the default — unchanged —
// separating "analogical neighbor" from "unrelated" with wide margins on both sides, and
// scaling with the build dimension like the distances it classifies.
const GeneralizationRadius = core.Dimension * 33 / 100

// Decision is the glass-box record of one action choice — the creature inspector renders it
// verbatim. Basis:
//   - "lesson"         — an exactly-matching stored lesson produced the action (Contributors
//     names it, probe distance 0);
//   - "generalization" — no exact match, but the winner's margin is decisive: the brain is
//     generalizing from similar taught percepts (Contributors names the nearest source
//     lessons within GeneralizationRadius). This is real VSA behavior — role-sharing
//     percepts partially match — surfaced honestly instead of suppressed;
//   - "instinct"       — nothing matched (empty brain or noise-level margin); the creature
//     defaults to wandering.
type Decision struct {
	Percept      PerceptSpec          `json:"percept"`
	Action       string               `json:"action"`
	Basis        string               `json:"basis"` // "lesson" | "generalization" | "instinct"
	Margin       int                  `json:"margin"`
	Candidates   []core.Candidate     `json:"candidates"`
	Contributors []engine.Contributor `json:"contributors,omitempty"`
}

// Brain is one creature's memory: an engine.AssociativeMemory over the shared vocabulary.
// Lessons are one-shot associations percept → action, individually removable, with full
// provenance.
type Brain struct {
	vocab *Vocab
	mem   *engine.AssociativeMemory
	epoch uint64 // bumped on every mutation; lets the world snapshot brain images lazily
}

// NewBrain creates an empty brain over the shared vocabulary.
func NewBrain(v *Vocab) *Brain {
	m := engine.NewAssociativeMemory()
	m.SetVocabSeed(v.Dict.Seed())
	return &Brain{vocab: v, mem: m}
}

// Epoch returns the brain's mutation counter (changes only on teach/transfer/forget/merge).
func (b *Brain) Epoch() uint64 { return b.epoch }

// Teach stores one lesson (one-shot — no training loop) and returns its ledger id.
func (b *Brain) Teach(p PerceptSpec, action string) (engine.AssociationID, error) {
	if err := ValidPercept(p); err != nil {
		return engine.AssociationID{}, err
	}
	actHV, err := b.vocab.ActionHV(action)
	if err != nil {
		return engine.AssociationID{}, err
	}
	label := fmt.Sprintf("%s → %s", p, action)
	b.epoch++
	return b.mem.StoreLabeled(b.vocab.EncodePercept(p), actHV, label), nil
}

// Transfer is the analogy verb: it derives a NEW lesson from an existing one by substituting
// the percept's subject (e.g. predator→guard), preserving every other role binding. The
// lesson's label records its parent, so lineage is visible in the ledger.
//
// Implementation note (honesty over cleverness): the new percept is re-encoded from the
// substituted symbols rather than XOR-patched into the stored bound pair — Bundle is a
// majority vote and is not XOR-invertible, so symbolic substitution + re-encoding is the
// exact operation, not an approximation. The unchanged roles produce literally identical
// bound vectors, which is what makes this a role-preserving analogy.
func (b *Brain) Transfer(lesson engine.AssociationID, newSees string) (engine.AssociationID, error) {
	var zero engine.AssociationID
	rec, err := b.findLesson(lesson)
	if err != nil {
		return zero, err
	}
	p, action, err := parseLessonLabel(rec.Label)
	if err != nil {
		return zero, err
	}
	if p.Sees == SeesNothing {
		return zero, fmt.Errorf("lesson %s has no substitutable subject", lesson)
	}
	oldSees := p.Sees
	p.Sees = newSees
	if err := ValidPercept(p); err != nil {
		return zero, err
	}
	actHV, err := b.vocab.ActionHV(action)
	if err != nil {
		return zero, err
	}
	label := fmt.Sprintf("%s → %s (from lesson %s: %s→%s)", p, action, lesson, oldSees, newSees)
	b.epoch++
	return b.mem.StoreLabeled(b.vocab.EncodePercept(p), actHV, label), nil
}

// Forget exactly unlearns one lesson: the memory becomes bit-identical to never having
// stored it (O(D) counter decrement in the engine).
func (b *Brain) Forget(lesson engine.AssociationID) error {
	if err := b.mem.RemoveAssociation(lesson); err != nil {
		return err
	}
	b.epoch++
	return nil
}

// Lessons returns the brain's ledger (id, label, removed) for the inspector.
func (b *Brain) Lessons() []engine.AssociationRecord {
	return b.mem.Ledger()
}

// Decide ranks the four actions against the unbound percept query and returns the full
// glass-box decision. Instinct rule: an empty brain, or a winner margin below
// MinDecisionMargin, falls back to wander — visible as basis "instinct" in the trace.
func (b *Brain) Decide(p PerceptSpec) Decision {
	perceptHV := b.vocab.EncodePercept(p)
	query := b.mem.Matrix().Bind(perceptHV)

	cands := make([]core.Candidate, len(Actions))
	for i, a := range Actions {
		hv, _ := b.vocab.ActionHV(a)
		cands[i] = core.Candidate{Token: a, Distance: core.HammingDistance(query, hv)}
	}
	for i := 1; i < len(cands); i++ { // insertion sort; ties keep Actions order (deterministic)
		for j := i; j > 0 && cands[j].Distance < cands[j-1].Distance; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}

	d := Decision{Percept: p, Candidates: cands}
	d.Margin = cands[1].Distance - cands[0].Distance

	if b.mem.Total() == 0 || d.Margin < MinDecisionMargin {
		d.Action = ActWander
		d.Basis = "instinct"
		return d
	}

	d.Action = cands[0].Token
	actHV, _ := b.vocab.ActionHV(d.Action)
	probe := perceptHV.Bind(actHV)

	// Exact recall names its lesson at probe distance 0. Otherwise this is an analogical
	// generalization — name the nearest source lessons so the trace still explains itself.
	if exact := b.mem.Contributors(probe, 0); len(exact) > 0 {
		d.Basis = "lesson"
		d.Contributors = exact
	} else if gen := b.mem.Contributors(probe, GeneralizationRadius); len(gen) > 0 {
		// A genuine analogical generalization NAMES its nearest source lessons. Real 2-of-3
		// role-sharing percepts land ~2500 bits, well inside the radius, so a real
		// generalization always finds a contributor here.
		d.Basis = "generalization"
		d.Contributors = gen
	} else {
		// Decisive margin but NO identifiable source within the generalization radius: this
		// is superposition interference, not generalization. Labeling it "generalization"
		// would break the glass-box promise that every generalized decision names its
		// source — so we treat an unexplained winner as instinct and do not act on it
		// confidently.
		d.Action = ActWander
		d.Basis = "instinct"
		d.Contributors = nil
	}
	return d
}

// Memory exposes the underlying associative memory (used for world hashing, merging, and
// receipts).
func (b *Brain) Memory() *engine.AssociativeMemory {
	return b.mem
}

// replaceMemory swaps in a new memory — the atomic-commit step of a multi-source brain
// merge (the scratch clone becomes live only after every source merged cleanly).
func (b *Brain) replaceMemory(m *engine.AssociativeMemory) {
	b.mem = m
	b.epoch++
}

func (b *Brain) findLesson(id engine.AssociationID) (engine.AssociationRecord, error) {
	for _, rec := range b.mem.Ledger() {
		if rec.ID == id {
			if rec.Removed {
				return rec, fmt.Errorf("lesson %s has been forgotten", id)
			}
			return rec, nil
		}
	}
	return engine.AssociationRecord{}, fmt.Errorf("unknown lesson %s", id)
}

// parseLessonLabel recovers the symbolic percept and action from a lesson label of the form
// "sees:<kind>,<dist>,<dir> → <action>" (transfer suffixes are ignored). Labels are the
// symbolic source of truth for transfers, so the format is deliberately rigid.
func parseLessonLabel(label string) (PerceptSpec, string, error) {
	var p PerceptSpec
	var rest string
	if n, _ := fmt.Sscanf(label, "sees:%s → %s", &p.Sees, &rest); n >= 2 {
		if p.Sees == SeesNothing {
			return p, firstField(rest), nil
		}
		// p.Sees currently holds "kind,dist,dir"
		var kind, dist, dir string
		if n, _ := fmt.Sscanf(p.Sees, "%s", &kind); n == 1 {
			parts := splitPercept(kind)
			if len(parts) == 3 {
				kind, dist, dir = parts[0], parts[1], parts[2]
				return PerceptSpec{Sees: kind, Dist: dist, Dir: dir}, firstField(rest), nil
			}
		}
		_ = dist
		_ = dir
	}
	return p, "", fmt.Errorf("unparseable lesson label %q", label)
}

func splitPercept(s string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return parts
}

func firstField(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return s[:i]
		}
	}
	return s
}
