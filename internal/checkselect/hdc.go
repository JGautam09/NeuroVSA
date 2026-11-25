package checkselect

import (
	"slices"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
)

const (
	hdcMinMargin = 200
	hdcRadius    = 3000
)

// hdcSelector augments rules for the benchmark. Required checks and fallback
// come from the rules. Only development examples contribute to the memory.
func hdcSelector(p Policy, cases []Case) func([]Change) Result {
	memory := engine.NewAssociativeMemory()
	var tokens []string
	for _, c := range cases {
		if c.Split != "development" || c.Expected.Fallback {
			continue
		}
		query := encodeChange(c.Changes)
		for _, id := range c.Expected.Optional {
			token := "check:" + id
			memory.StoreLabeled(query, core.SeededHV(core.DefaultSeed, token), c.ID)
			tokens = append(tokens, token)
		}
	}
	tokens = sortedUnique(tokens)
	return func(changes []Change) Result {
		result := p.Recommend(changes)
		if result.Fallback != "" || len(tokens) < 2 || memory.Total() > engine.RecommendedMaxActiveAssociations {
			return result
		}
		proof, err := engine.IssueDecision(memory, encodeChange(changes), tokens, hdcRadius)
		if err != nil || proof.Candidates[1].Distance-proof.Distance < hdcMinMargin || len(proof.Contributors) == 0 {
			return result
		}
		id := proof.Chosen[len("check:"):]
		if !slices.Contains(result.Required, id) && !slices.Contains(result.Optional, id) {
			result.Optional = sortedUnique(append(result.Optional, id))
			result.HDCProof = &proof
		}
		return result
	}
}

func encodeChange(changes []Change) core.Hypervector {
	tokens := features(changes)
	items := make([]core.Hypervector, len(tokens))
	for i, token := range tokens {
		items[i] = core.SeededHV(core.DefaultSeed, "change:"+token)
	}
	return core.Bundle(items)
}
