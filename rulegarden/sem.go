package rulegarden

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JGautam09/NeuroVSA/engine"
)

// Lesson semantics (the structural P1-4 fix): every lesson carries a machine-readable
// record of what it MEANS — percept, action, and transfer lineage — in the engine's `sem`
// field, where pack signatures cover it, fingerprints hash it, and import validation
// re-derives the bound vector from it. The display label is exactly that: display. Before
// this, Transfer parsed semantics out of the label string, and a label was only as
// trustworthy as the importer's string validation (see docs/security/REVIEW-2026-07.md).

// SemDomain tags RuleGarden lesson sems so generic tools (nvsa-verify) know which
// validator applies. Unknown domains are reported as present-but-not-verifiable, never
// silently accepted as verified.
const SemDomain = "rulegarden"

// semVersion is the lesson-sem schema version. Parsing is strict (unknown fields are
// errors), so schema changes must bump this rather than extend silently.
const semVersion = 1

// LessonSem is the machine-readable semantics of one lesson.
type LessonSem struct {
	V       int         `json:"v"`
	Domain  string      `json:"domain"`
	Percept PerceptSpec `json:"percept"`
	Action  string      `json:"action"`
	// Parent is the lesson this one was transferred from ("site:seq"), empty for direct
	// teaches — lineage as data, not label decoration.
	Parent string `json:"parent,omitempty"`
}

// EncodeLessonSem renders the canonical sem JSON for a lesson. Field order is fixed by
// the struct and the values are plain ASCII tokens, so equal semantics encode to equal
// bytes — which merge content-equality and fingerprint hashing depend on.
func EncodeLessonSem(p PerceptSpec, action, parent string) string {
	b, err := json.Marshal(LessonSem{V: semVersion, Domain: SemDomain, Percept: p, Action: action, Parent: parent})
	if err != nil {
		// Plain string-field structs cannot fail to marshal; a failure here is memory
		// corruption, not input.
		panic(fmt.Sprintf("EncodeLessonSem: %v", err))
	}
	return string(b)
}

// ParseLessonSem decodes and structurally validates a sem string: schema version, domain,
// a valid percept, a known action token, and a well-formed parent id when present. It does
// NOT check the sem against a bound vector — that is validateLessonPackContent's job, with
// a vocabulary in hand. Parsing is strict: unknown fields and trailing data are errors,
// because a sem is a signed trust-boundary input, not a config file.
func ParseLessonSem(s string) (LessonSem, error) {
	var ls LessonSem
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ls); err != nil {
		return ls, fmt.Errorf("invalid lesson sem: %w", err)
	}
	if dec.More() {
		return ls, fmt.Errorf("invalid lesson sem: trailing data after the JSON object")
	}
	if ls.V != semVersion {
		return ls, fmt.Errorf("unsupported lesson sem version %d (this build reads v%d)", ls.V, semVersion)
	}
	if ls.Domain != SemDomain {
		return ls, fmt.Errorf("lesson sem domain %q is not %q", ls.Domain, SemDomain)
	}
	if err := ValidPercept(ls.Percept); err != nil {
		return ls, fmt.Errorf("invalid lesson sem percept: %w", err)
	}
	okAction := false
	for _, a := range Actions {
		if a == ls.Action {
			okAction = true
			break
		}
	}
	if !okAction {
		return ls, fmt.Errorf("invalid lesson sem: unknown action %q", ls.Action)
	}
	if ls.Parent != "" {
		var id engine.AssociationID
		if err := id.UnmarshalText([]byte(ls.Parent)); err != nil {
			return ls, fmt.Errorf("invalid lesson sem parent: %w", err)
		}
	}
	return ls, nil
}

// LessonProvenance is the display summary of one ledger record's provenance. Structured
// lessons expose their machine-verified percept/action/parent; entries imported from
// legacy (pre-sem) packs are flagged so a UI never presents a display label as verified
// meaning.
type LessonProvenance struct {
	Structured bool   `json:"structured"`
	Percept    string `json:"percept,omitempty"` // canonical "sees:..." rendering
	Action     string `json:"action,omitempty"`
	Parent     string `json:"parent,omitempty"`
}

// ProvenanceOf derives the display provenance of a ledger record from its sem field.
func ProvenanceOf(rec engine.AssociationRecord) LessonProvenance {
	if rec.Sem == "" {
		return LessonProvenance{}
	}
	ls, err := ParseLessonSem(rec.Sem)
	if err != nil {
		return LessonProvenance{}
	}
	return LessonProvenance{Structured: true, Percept: ls.Percept.String(), Action: ls.Action, Parent: ls.Parent}
}
