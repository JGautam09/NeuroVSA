package engine

import (
	"github.com/JGautam09/NeuroVSA/core"
)

// DefaultStopThreshold is the normalized Hamming distance above which a prediction is
// treated as noise. Quasi-orthogonal (random) vectors sit at ~0.5; a genuine recovered
// token is well below that. 0.42 cleanly separates real recoveries (~0.31) from the noise
// floor (~0.49) observed in practice.
const DefaultStopThreshold = 0.42

// HDCDecoder handles sequence generation and token prediction using HDC unbinding math.
type HDCDecoder struct {
	Memory *AssociativeMemory
	Dict   *core.TokenDictionary
	// StopThreshold halts generation when the best match's normalized distance exceeds it,
	// so the loop stops at low confidence instead of emitting noise tokens.
	StopThreshold float64
}

// NewHDCDecoder creates a new HDCDecoder instance.
func NewHDCDecoder(mem *AssociativeMemory, dict *core.TokenDictionary) *HDCDecoder {
	return &HDCDecoder{
		Memory:        mem,
		Dict:          dict,
		StopThreshold: DefaultStopThreshold,
	}
}

// EncodeContext builds a context hypervector from an ordered token sequence using the same
// permute-then-bind recurrence used during training and autoregressive generation:
//
//	ctx = HV(t0); for ti in t1..: ctx = ρ(ctx) ⊗ HV(ti)
//
// Using one canonical encoder for both training and inference is what makes multi-token
// seeds recover cleanly — a bundle-of-permuted-tokens seed would not align with the stored
// contexts.
func EncodeContext(dict *core.TokenDictionary, tokens []string) core.Hypervector {
	if len(tokens) == 0 {
		return core.ZeroHV()
	}
	ctx := dict.GetOrRegister(tokens[0])
	for _, tok := range tokens[1:] {
		ctx = ctx.Permute(1).Bind(dict.GetOrRegister(tok))
	}
	return ctx
}

// PredictNextToken performs VSA unbinding and clean-up lookup:
// 1. V_query = V_memory ⊗ V_context
// 2. Parallel Hamming distance search in TokenDictionary to find closest match.
func (dec *HDCDecoder) PredictNextToken(contextHV core.Hypervector) (string, int) {
	memMatrix := dec.Memory.Matrix()

	// Unbind query vector: V_query = V_memory ⊗ V_context
	queryHV := memMatrix.Bind(contextHV)

	// Clean-up lookup against crisp token dictionary
	token, dist := dec.Dict.LookupToken(queryHV)
	return token, dist
}

// GenerateSequence runs an autoregressive sequence prediction loop.
// Updates context vector at each step: V_context' = ρ(V_context) ⊗ V_token.
// It delegates to GenerateSequenceTraced (candidate table capped at 1, no contributor scan),
// so traced and untraced generation share one implementation and cannot diverge.
func (dec *HDCDecoder) GenerateSequence(startContext core.Hypervector, maxTokens int) ([]string, []int) {
	sequence, distances, _ := dec.GenerateSequenceTraced(startContext, maxTokens, 1, false)
	return sequence, distances
}
