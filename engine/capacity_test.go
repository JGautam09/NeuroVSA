package engine

import (
	"fmt"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// G0 capacity gate: how many (context → action) associations fit in ONE AssociativeMemory
// before recall degrades? This bounds RuleGarden brain sizes, NeuroMesh merge warnings, and
// the honest-limits documentation. Cleanup ranks ONLY the action vocabulary (4 actions),
// mirroring how a creature brain decides — not a full-dictionary search.
//
// Two variants:
//   - random: quasi-orthogonal contexts — the theoretical upper bound.
//   - structured: RuleGarden-style percepts, Bundle(see⊗entity, dist⊗d, dir⊗w), which share
//     role fillers and therefore interfere — the realistic (harder) case.

var capacityActions = []string{"move-toward", "move-away", "eat", "wander"}

func actionVectors(dict *core.TokenDictionary) []core.Hypervector {
	out := make([]core.Hypervector, len(capacityActions))
	for i, a := range capacityActions {
		out[i] = dict.GetOrRegister("action:" + a)
	}
	return out
}

// recallAccuracy stores each context with an assigned action, then checks that unbinding the
// memory with each context ranks the assigned action first among the 4 actions. Returns
// accuracy and the mean margin (runner-up distance − winner distance) over correct recalls.
func recallAccuracy(t *testing.T, contexts []core.Hypervector, dict *core.TokenDictionary) (float64, float64) {
	t.Helper()
	mem := NewAssociativeMemory()
	acts := actionVectors(dict)

	assigned := make([]int, len(contexts))
	for i, ctx := range contexts {
		assigned[i] = i % len(acts)
		mem.StoreLabeled(ctx, acts[assigned[i]], fmt.Sprintf("cap-%d", i))
	}

	matrix := mem.Matrix()
	correct := 0
	marginSum := 0.0
	for i, ctx := range contexts {
		query := matrix.Bind(ctx)
		best, second, bestIdx := core.Dimension+1, core.Dimension+1, -1
		for a := range acts {
			d := core.HammingDistance(query, acts[a])
			if d < best {
				second = best
				best, bestIdx = d, a
			} else if d < second {
				second = d
			}
		}
		if bestIdx == assigned[i] {
			correct++
			marginSum += float64(second - best)
		}
	}
	acc := float64(correct) / float64(len(contexts))
	meanMargin := 0.0
	if correct > 0 {
		meanMargin = marginSum / float64(correct)
	}
	return acc, meanMargin
}

func TestCapacityCurveRandomContexts(t *testing.T) {
	if core.Dimension != 10000 {
		t.Skip("the G0 capacity envelope is measured at the default dimension; study builds re-measure it (docs/DIMENSIONALITY.md)")
	}
	dict := core.NewTokenDictionary()
	t.Log("G0 capacity — RANDOM (quasi-orthogonal) contexts, 4-action cleanup")
	t.Log("   K    accuracy   mean margin (bits)")
	for _, k := range []int{4, 8, 16, 32, 64, 128, 256, 512} {
		contexts := make([]core.Hypervector, k)
		for i := range contexts {
			contexts[i] = core.SeededHV(7, fmt.Sprintf("ctx-%d-%d", k, i))
		}
		acc, margin := recallAccuracy(t, contexts, dict)
		t.Logf("%5d   %6.1f%%   %8.1f", k, 100*acc, margin)
		if k <= 64 && acc < 0.99 {
			t.Errorf("random-context recall at K=%d fell below 99%%: %.1f%%", k, 100*acc)
		}
	}
}

func TestCapacityCurveStructuredPercepts(t *testing.T) {
	dict := core.NewTokenDictionary()
	roleSee := dict.GetOrRegister("role:sees")
	roleDist := dict.GetOrRegister("role:dist")
	roleDir := dict.GetOrRegister("role:dir")

	// RuleGarden MVP-scale vocabulary: 12 entity types × 2 distances × 4 directions = 96
	// distinct percepts, all sharing fillers (the interference-prone regime).
	var percepts []core.Hypervector
	for e := 0; e < 12; e++ {
		entity := dict.GetOrRegister(fmt.Sprintf("entity:%d", e))
		for _, d := range []string{"near", "far"} {
			distHV := dict.GetOrRegister("dist:" + d)
			for _, w := range []string{"N", "S", "E", "W"} {
				dirHV := dict.GetOrRegister("dir:" + w)
				percepts = append(percepts, core.Bundle([]core.Hypervector{
					roleSee.Bind(entity), roleDist.Bind(distHV), roleDir.Bind(dirHV),
				}))
			}
		}
	}

	t.Log("G0 capacity — STRUCTURED percepts (shared fillers), 4-action cleanup")
	t.Log("   K    accuracy   mean margin (bits)")
	for _, k := range []int{4, 8, 16, 24, 32, 48, 64, 96} {
		acc, margin := recallAccuracy(t, percepts[:k], dict)
		t.Logf("%5d   %6.1f%%   %8.1f", k, 100*acc, margin)
		if k <= 24 && acc < 0.99 {
			t.Errorf("structured-percept recall at K=%d fell below 99%%: %.1f%%", k, 100*acc)
		}
	}
}
