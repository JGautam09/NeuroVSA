package arena

import (
	"regexp"
	"strings"

	"github.com/JGautam09/NeuroVSA/core"
)

// Encoder maps natural-language utterances to hypervectors using a deterministic, seeded item
// memory and an n-gram binding scheme.
//
// "Deterministic" is load-bearing for the arena's determinism axis: each token's base
// hypervector is derived from a hash of the token string (a seeded splitmix64 stream), NOT
// from crypto/rand. So the same utterance encodes to a bit-identical vector on every run and
// every machine — a property a floating-point neural encoder cannot guarantee across builds,
// hardware, or library versions.
type Encoder struct {
	cache map[string]core.Hypervector
}

// NewEncoder returns an encoder with an empty token cache.
func NewEncoder() *Encoder {
	return &Encoder{cache: make(map[string]core.Hypervector)}
}

var tokenSplit = regexp.MustCompile(`[a-z0-9]+`)

func tokenize(s string) []string {
	return tokenSplit.FindAllString(strings.ToLower(s), -1)
}

// tokenHV derives a token's base hypervector via core.SeededHV under core.DefaultSeed —
// seed 0 is bit-compatible with this encoder's original private hash stream, so the arena's
// committed reference results remain valid. Results are cached (still deterministic).
func (e *Encoder) tokenHV(tok string) core.Hypervector {
	if hv, ok := e.cache[tok]; ok {
		return hv
	}
	hv := core.SeededHV(core.DefaultSeed, tok)
	e.cache[tok] = hv
	return hv
}

// Encode builds an utterance hypervector as the bundle of its unigram, bigram, and trigram
// features. N-grams use position-permuted binding so word order is preserved:
//
//	bigram(a,b)    = ρ¹(a) ⊗ b
//	trigram(a,b,c) = ρ²(a) ⊗ ρ¹(b) ⊗ c
//
// This is the standard n-gram HDC text encoding (cf. HDC language identification). It matches
// surface form well and — by design, not by weakness — has no semantic generalization, which
// is exactly the property the canonical-vs-paraphrase split is meant to expose.
func (e *Encoder) Encode(utterance string) core.Hypervector {
	toks := tokenize(utterance)
	if len(toks) == 0 {
		return core.ZeroHV()
	}

	feats := make([]core.Hypervector, 0, len(toks)*3)
	for _, t := range toks {
		feats = append(feats, e.tokenHV(t))
	}
	for i := 0; i+1 < len(toks); i++ {
		a := e.tokenHV(toks[i]).Permute(1)
		feats = append(feats, a.Bind(e.tokenHV(toks[i+1])))
	}
	for i := 0; i+2 < len(toks); i++ {
		a := e.tokenHV(toks[i]).Permute(2)
		b := e.tokenHV(toks[i+1]).Permute(1)
		feats = append(feats, a.Bind(b).Bind(e.tokenHV(toks[i+2])))
	}
	return core.Bundle(feats)
}
