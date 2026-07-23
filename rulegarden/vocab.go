package rulegarden

import (
	"fmt"

	"github.com/JGautam09/NeuroVSA/core"
)

// VocabSeed is the fixed item-memory seed shared by ALL RuleGarden worlds. It is deliberately
// not the world seed: the world seed drives only the PRNG (spawns, wandering), while the
// vocabulary must be identical everywhere so that (a) packs replay bit-exactly on any machine
// and (b) brains from different worlds stay mergeable (Phase 2 merge requires equal vocab
// seeds).
const VocabSeed uint64 = core.DefaultSeed

// Kind is an entity type token. The vocabulary is deliberately bounded (the arena lesson:
// bounded symbolic input is HDC's home turf).
type Kind string

const (
	KindFood     Kind = "food"
	KindPredator Kind = "predator"
	KindGuard    Kind = "guard"
	KindPrey     Kind = "prey"
	KindIntruder Kind = "intruder"
	KindWater    Kind = "water"
)

// Kinds is the full spawnable vocabulary.
var Kinds = []Kind{KindFood, KindPredator, KindGuard, KindPrey, KindIntruder, KindWater}

// SeesNothing is the percept subject when no object is in the world.
const SeesNothing = "nothing"

// Actions (4, per the locked MVP scope).
const (
	ActMoveToward = "move-toward"
	ActMoveAway   = "move-away"
	ActEat        = "eat"
	ActWander     = "wander"
)

// Actions is the brain's cleanup vocabulary — decisions rank exactly these four.
var Actions = []string{ActMoveToward, ActMoveAway, ActEat, ActWander}

// Percept distance and direction buckets.
const (
	DistNear = "near"
	DistFar  = "far"
)

var Dirs = []string{"N", "S", "E", "W"}

// PerceptSpec is the symbolic form of a percept — what event logs and teaching UIs carry.
// Vectors are derived from it deterministically at apply time, never stored in packs.
type PerceptSpec struct {
	Sees string `json:"sees"`           // Kind or SeesNothing
	Dist string `json:"dist,omitempty"` // DistNear | DistFar (empty when Sees == nothing)
	Dir  string `json:"dir,omitempty"`  // N | S | E | W        (empty when Sees == nothing)
}

func (p PerceptSpec) String() string {
	if p.Sees == SeesNothing {
		return "sees:nothing"
	}
	return fmt.Sprintf("sees:%s,%s,%s", p.Sees, p.Dist, p.Dir)
}

// Vocab owns the shared token dictionary and the percept/action encoders.
type Vocab struct {
	Dict     *core.TokenDictionary
	roleSee  core.Hypervector
	roleDist core.Hypervector
	roleDir  core.Hypervector
	actions  map[string]core.Hypervector
}

// NewVocab builds the RuleGarden vocabulary on the fixed VocabSeed, pre-registering every
// token so dictionaries are content-identical regardless of play order.
func NewVocab() *Vocab {
	d := core.NewSeededTokenDictionary(VocabSeed)
	v := &Vocab{
		Dict:     d,
		roleSee:  d.GetOrRegister("role:sees"),
		roleDist: d.GetOrRegister("role:dist"),
		roleDir:  d.GetOrRegister("role:dir"),
		actions:  make(map[string]core.Hypervector, len(Actions)),
	}
	for _, k := range Kinds {
		d.GetOrRegister("entity:" + string(k))
	}
	d.GetOrRegister("entity:" + SeesNothing)
	d.GetOrRegister("dist:" + DistNear)
	d.GetOrRegister("dist:" + DistFar)
	for _, dir := range Dirs {
		d.GetOrRegister("dir:" + dir)
	}
	for _, a := range Actions {
		v.actions[a] = d.GetOrRegister("action:" + a)
	}
	return v
}

// EncodePercept maps a symbolic percept to its hypervector:
//
//	Bundle( role:sees ⊗ HV(entity), role:dist ⊗ HV(dist), role:dir ⊗ HV(dir) )
//
// This is exactly the structured-percept encoding validated by the G0 capacity gate (100%
// recall across the full percept space with ~840-bit margins).
func (v *Vocab) EncodePercept(p PerceptSpec) core.Hypervector {
	if p.Sees == SeesNothing {
		return v.roleSee.Bind(v.Dict.GetOrRegister("entity:" + SeesNothing))
	}
	return core.Bundle([]core.Hypervector{
		v.roleSee.Bind(v.Dict.GetOrRegister("entity:" + p.Sees)),
		v.roleDist.Bind(v.Dict.GetOrRegister("dist:" + p.Dist)),
		v.roleDir.Bind(v.Dict.GetOrRegister("dir:" + p.Dir)),
	})
}

// ActionHV returns the vector for an action token.
func (v *Vocab) ActionHV(action string) (core.Hypervector, error) {
	hv, ok := v.actions[action]
	if !ok {
		return core.Hypervector{}, fmt.Errorf("unknown action %q", action)
	}
	return hv, nil
}

// ValidPercept validates a symbolic percept against the bounded vocabulary.
func ValidPercept(p PerceptSpec) error {
	if p.Sees == SeesNothing {
		return nil
	}
	okKind := false
	for _, k := range Kinds {
		if string(k) == p.Sees {
			okKind = true
			break
		}
	}
	if !okKind {
		return fmt.Errorf("unknown entity kind %q", p.Sees)
	}
	if p.Dist != DistNear && p.Dist != DistFar {
		return fmt.Errorf("unknown distance %q", p.Dist)
	}
	okDir := false
	for _, d := range Dirs {
		if d == p.Dir {
			okDir = true
			break
		}
	}
	if !okDir {
		return fmt.Errorf("unknown direction %q", p.Dir)
	}
	return nil
}
