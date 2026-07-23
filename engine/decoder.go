package engine

import (
	"neuro-vsa/core"
)

// HDCDecoder handles sequence generation and token prediction using HDC unbinding math.
type HDCDecoder struct {
	Memory *AssociativeMemory
	Dict   *core.TokenDictionary
}

// NewHDCDecoder creates a new HDCDecoder instance.
func NewHDCDecoder(mem *AssociativeMemory, dict *core.TokenDictionary) *HDCDecoder {
	return &HDCDecoder{
		Memory: mem,
		Dict:   dict,
	}
}

// PredictNextToken performs VSA unbinding and clean-up lookup:
// 1. V_query = V_memory ⊗ V_context
// 2. Parallel Hamming distance search in TokenDictionary to find closest match.
func (dec *HDCDecoder) PredictNextToken(contextHV core.Hypervector) (string, int) {
	dec.Memory.mu.RLock()
	memMatrix := dec.Memory.MemoryMatrix
	dec.Memory.mu.RUnlock()

	// Unbind query vector: V_query = V_memory ⊗ V_context
	queryHV := memMatrix.Bind(contextHV)

	// Clean-up lookup against crisp token dictionary
	token, dist := dec.Dict.LookupToken(queryHV)
	return token, dist
}

// GenerateSequence runs an autoregressive sequence prediction loop.
// Updates context vector at each step: V_context' = ρ(V_context) ⊗ V_token.
func (dec *HDCDecoder) GenerateSequence(startContext core.Hypervector, maxTokens int) ([]string, []int) {
	var sequence []string
	var distances []int

	currContext := startContext

	for step := 0; step < maxTokens; step++ {
		token, dist := dec.PredictNextToken(currContext)
		if token == "" || token == "<END>" {
			break
		}

		sequence = append(sequence, token)
		distances = append(distances, dist)

		// Get hypervector for predicted token
		tokenHV := dec.Dict.GetOrRegister(token)

		// Autoregressive context shift and bind: V_context' = ρ(V_context) ⊗ V_token
		currContext = currContext.Permute(1).Bind(tokenHV)
	}

	return sequence, distances
}
