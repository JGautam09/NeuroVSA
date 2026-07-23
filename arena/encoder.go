package arena

import (
	"hash/fnv"
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

// tokenHV deterministically derives a token's base hypervector from a seeded splitmix64
// stream keyed by the FNV-1a hash of the token. Results are cached (still deterministic).
func (e *Encoder) tokenHV(tok string) core.Hypervector {
	if hv, ok := e.cache[tok]; ok {
		return hv
	}
	h := fnv.New64a()
	h.Write([]byte(tok))
	x := h.Sum64()

	var hv core.Hypervector
	for i := 0; i < core.NumWords; i++ {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z = z ^ (z >> 31)
		hv.Vector[i] = z
	}
	hv.Vector[core.NumWords-1] &= core.LastWordMask
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
