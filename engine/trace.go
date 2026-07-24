package engine

import (
	"fmt"

	"github.com/JGautam09/NeuroVSA/core"
)

// Stop reasons reported by GenerateSequenceTraced.
const (
	StopEndToken   = "end_token"   // the trained <END> marker was recovered
	StopNoiseFloor = "noise_floor" // best candidate exceeded the decoder's StopThreshold
	StopMaxTokens  = "max_tokens"  // the caller's token budget was exhausted
	StopEmptyDict  = "empty_dictionary"
)

// PredictionTrace is the first-class derivation record for one cleanup-based prediction.
// Every field is exact data from the actual computation — the symbolic ops applied, the full
// ranked candidate table, and (via the ledger) the precise stored association that produced
// the winner — not a post-hoc explanation.
type PredictionTrace struct {
	ContextTokens []string         `json:"context_tokens,omitempty"` // seed tokens, when known
	ContextOps    []string         `json:"context_ops,omitempty"`    // symbolic derivation of the query
	MemoryTotal   uint64           `json:"memory_total"`             // active associations consulted
	Candidates    []core.Candidate `json:"candidates"`               // ranked cleanup table
	Chosen        string           `json:"chosen"`
	Distance      int              `json:"distance"`
	Normalized    float64          `json:"normalized"`
	Contributors  []Contributor    `json:"contributors,omitempty"` // ledger entries at distance 0 from ctx ⊗ HV(chosen)
	StopReason    string           `json:"stop_reason,omitempty"`
}

// GenerationTrace is the step-by-step record of one autoregressive generation run.
type GenerationTrace struct {
	Steps      []PredictionTrace `json:"steps"`
	StopReason string            `json:"stop_reason"`
}

// EncodeContextTraced is EncodeContext plus the symbolic derivation of the returned vector.
func EncodeContextTraced(dict *core.TokenDictionary, tokens []string) (core.Hypervector, []string) {
	if len(tokens) == 0 {
		return core.ZeroHV(), []string{"ctx ← 0 (empty seed)"}
	}
	ops := make([]string, 0, len(tokens))
	ctx := dict.GetOrRegister(tokens[0])
	ops = append(ops, fmt.Sprintf("ctx ← HV(%q)", tokens[0]))
	for _, tok := range tokens[1:] {
		ctx = ctx.Permute(1).Bind(dict.GetOrRegister(tok))
		ops = append(ops, fmt.Sprintf("ctx ← ρ¹(ctx) ⊗ HV(%q)", tok))
	}
	return ctx, ops
}

// PredictNextTokenTraced performs the same unbind + cleanup as PredictNextToken and returns
// the full derivation. topK bounds the candidate table (<= 0 for all tokens). When
// withContributors is set, the memory's ledger is scanned for the exact stored association
// behind the winner (probe = ctx ⊗ HV(chosen), distance 0).
func (dec *HDCDecoder) PredictNextTokenTraced(contextHV core.Hypervector, topK int, withContributors bool) PredictionTrace {
	memMatrix := dec.Memory.Matrix()
	queryHV := memMatrix.Bind(contextHV)

	tr := PredictionTrace{
		ContextOps:  []string{"query ← M ⊗ ctx"},
		MemoryTotal: dec.Memory.Total(),
		Candidates:  dec.Dict.LookupCandidates(queryHV, topK),
		Distance:    -1,
	}
	if len(tr.Candidates) == 0 {
		return tr
	}
	tr.Chosen = tr.Candidates[0].Token
	tr.Distance = tr.Candidates[0].Distance
	tr.Normalized = float64(tr.Distance) / float64(core.Dimension)

	if withContributors && tr.Chosen != "" {
		probe := contextHV.Bind(dec.Dict.GetOrRegister(tr.Chosen))
		tr.Contributors = dec.Memory.Contributors(probe, 0)
	}
	return tr
}

// GenerateSequenceTraced runs the autoregressive loop with a per-step trace and an explicit
// stop reason. It is the single implementation of generation — GenerateSequence delegates
// here — so traced and untraced behavior cannot diverge.
func (dec *HDCDecoder) GenerateSequenceTraced(startContext core.Hypervector, maxTokens, topK int, withContributors bool) ([]string, []int, GenerationTrace) {
	var sequence []string
	var distances []int
	gt := GenerationTrace{StopReason: StopMaxTokens}

	currContext := startContext
	for step := 0; step < maxTokens; step++ {
		tr := dec.PredictNextTokenTraced(currContext, topK, withContributors)

		switch {
		case tr.Chosen == "":
			tr.StopReason = StopEmptyDict
		case tr.Chosen == "<END>":
			tr.StopReason = StopEndToken
		case tr.Normalized > dec.StopThreshold:
			tr.StopReason = StopNoiseFloor
		}
		if tr.StopReason != "" {
			gt.Steps = append(gt.Steps, tr)
			gt.StopReason = tr.StopReason
			return sequence, distances, gt
		}

		sequence = append(sequence, tr.Chosen)
		distances = append(distances, tr.Distance)
		gt.Steps = append(gt.Steps, tr)

		tokenHV := dec.Dict.GetOrRegister(tr.Chosen)
		currContext = currContext.Permute(1).Bind(tokenHV)
	}
	return sequence, distances, gt
}

// SelectNextToolCertified resolves the next tool like SelectNextTool and additionally
// issues a replay-verifiable DecisionCertificate over the policy memory: the receipt any
// holder of the policy pack can re-execute to bit-exact agreement.
func (tt *TrajectoryTracker) SelectNextToolCertified() (string, PredictionTrace, DecisionCertificate, error) {
	// State capture and the traced decision happen under ONE lock acquisition, so the
	// certificate's state is exactly the decision's state; IssueDecision then re-ranks with
	// the identical tie-break rule, so certificate and live decision cannot disagree.
	tt.mu.Lock()
	state := tt.CurrentState
	tool, trace := tt.selectTracedLocked()
	tt.mu.Unlock()

	// Router certificates use exact-only contributors (radius 0): tool routing has no
	// analogical-generalization basis, so a decision is lesson or instinct, never dressed up.
	cert, err := IssueDecision(tt.router.policy, state, tt.router.tools, 0)
	if err != nil {
		return tool, trace, DecisionCertificate{}, err
	}
	if cert.Chosen != tool {
		// Unreachable by construction (same ranking rule); guard loudly rather than ship a
		// receipt that contradicts the action.
		return tool, trace, DecisionCertificate{}, fmt.Errorf("certificate/decision divergence: %q vs %q", cert.Chosen, tool)
	}
	return tool, trace, cert, nil
}

// SelectNextToolTraced resolves the next tool exactly like SelectNextTool and additionally
// returns the ranked tool table plus the workflow-step association (from the policy ledger)
// that produced the decision.
func (tt *TrajectoryTracker) SelectNextToolTraced() (string, PredictionTrace) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.selectTracedLocked()
}

// selectTracedLocked is the traced selection core; caller must hold tt.mu.
func (tt *TrajectoryTracker) selectTracedLocked() (string, PredictionTrace) {
	r := tt.router
	query := r.policy.Matrix().Bind(tt.CurrentState)

	// Rank all tools; ties resolve by registration order, matching predict()'s argmin scan.
	cands := make([]core.Candidate, len(r.tools))
	for i, tool := range r.tools {
		cands[i] = core.Candidate{Token: tool, Distance: core.HammingDistance(query, r.toolDict.GetOrRegister(tool))}
	}
	for i := 1; i < len(cands); i++ { // insertion sort keeps the stable, index-ordered tie-break
		for j := i; j > 0 && cands[j].Distance < cands[j-1].Distance; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}

	tr := PredictionTrace{
		ContextOps:  []string{fmt.Sprintf("goal %q, %d actions recorded", tt.goal, len(tt.ActionLog)), "query ← Policy ⊗ state"},
		MemoryTotal: r.policy.Total(),
		Candidates:  cands,
		Distance:    -1,
	}
	if len(cands) == 0 {
		return "", tr
	}
	tr.Chosen = cands[0].Token
	tr.Distance = cands[0].Distance
	tr.Normalized = float64(tr.Distance) / float64(core.Dimension)
	tr.Contributors = r.policy.Contributors(tt.CurrentState.Bind(r.toolDict.GetOrRegister(tr.Chosen)), 0)
	return tr.Chosen, tr
}
