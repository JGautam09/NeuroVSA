package core

import (
	"sort"
	"sync"
)

// TokenDictionary maintains a thread-safe mapping between crisp string tokens/AST identifiers
// and their assigned base Hypervectors (Item Memory / Cleanup Memory).
//
// By default vectors are derived deterministically from (seed, token) via SeededHV, so two
// processes — on any machine — assign bit-identical vectors to the same token. This is what
// makes persisted memories usable across process restarts. NewRandomTokenDictionary restores
// the legacy crypto/rand behavior for callers that explicitly want unpredictable vectors.
type TokenDictionary struct {
	mu        sync.RWMutex
	seed      uint64
	random    bool // legacy mode: crypto/rand vectors (not reproducible across processes)
	tokenToHV map[string]Hypervector
	hvToToken []tokenPair
}

type tokenPair struct {
	Token string
	HV    Hypervector
}

// NewTokenDictionary initializes a deterministic TokenDictionary seeded with DefaultSeed.
// Since v0.2.0 this is the default: token vectors are bit-identical across runs and machines.
func NewTokenDictionary() *TokenDictionary {
	return NewSeededTokenDictionary(DefaultSeed)
}

// NewSeededTokenDictionary initializes a deterministic TokenDictionary: every token's vector
// is SeededHV(seed, token), reproducible on any machine that uses the same seed.
func NewSeededTokenDictionary(seed uint64) *TokenDictionary {
	return &TokenDictionary{
		seed:      seed,
		tokenToHV: make(map[string]Hypervector),
		hvToToken: make([]tokenPair, 0),
	}
}

// NewRandomTokenDictionary initializes a dictionary with the pre-v0.2.0 behavior: vectors are
// drawn from crypto/rand and are NOT reproducible across processes. Persisted memories built
// against a random dictionary cannot be reloaded meaningfully in a new process.
func NewRandomTokenDictionary() *TokenDictionary {
	return &TokenDictionary{
		random:    true,
		tokenToHV: make(map[string]Hypervector),
		hvToToken: make([]tokenPair, 0),
	}
}

// Seed returns the dictionary's item-memory seed (meaningful only for seeded dictionaries).
func (td *TokenDictionary) Seed() uint64 {
	return td.seed
}

// GetOrRegister retrieves the hypervector for a given token string, deriving and storing it
// on first use — deterministically via SeededHV(seed, token) in the default seeded mode, or
// from crypto/rand for NewRandomTokenDictionary instances.
func (td *TokenDictionary) GetOrRegister(token string) Hypervector {
	td.mu.Lock()
	defer td.mu.Unlock()

	if hv, exists := td.tokenToHV[token]; exists {
		return hv
	}

	var newHV Hypervector
	if td.random {
		newHV = GenerateRandom()
	} else {
		newHV = SeededHV(td.seed, token)
	}
	td.tokenToHV[token] = newHV
	td.hvToToken = append(td.hvToToken, tokenPair{Token: token, HV: newHV})
	return newHV
}

// Candidate is one cleanup-search result: a dictionary token and its Hamming distance from
// the query. Candidate tables are the raw material of glass-box traces.
type Candidate struct {
	Token    string `json:"token"`
	Distance int    `json:"distance"`
}

// LookupCandidates performs the cleanup search and returns the full ranked candidate table,
// sorted by ascending distance (ties broken by registration order, matching LookupToken's
// historical first-minimum semantics). k > 0 limits the table to the k nearest; k <= 0
// returns every registered token.
func (td *TokenDictionary) LookupCandidates(query Hypervector, k int) []Candidate {
	td.mu.RLock()
	defer td.mu.RUnlock()

	if len(td.hvToToken) == 0 {
		return nil
	}

	type ranked struct {
		Candidate
		idx int
	}
	all := make([]ranked, len(td.hvToToken))
	for i, pair := range td.hvToToken {
		all[i] = ranked{Candidate{Token: pair.Token, Distance: HammingDistance(query, pair.HV)}, i}
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].Distance != all[b].Distance {
			return all[a].Distance < all[b].Distance
		}
		return all[a].idx < all[b].idx
	})

	if k > 0 && k < len(all) {
		all = all[:k]
	}
	out := make([]Candidate, len(all))
	for i := range all {
		out[i] = all[i].Candidate
	}
	return out
}

// LookupToken performs a minimum Hamming distance search across clean dictionary entries
// to clean up a noisy query hypervector into its exact token string representation.
// Returns the token string and the minimum Hamming distance.
func (td *TokenDictionary) LookupToken(query Hypervector) (string, int) {
	cands := td.LookupCandidates(query, 1)
	if len(cands) == 0 {
		return "", -1
	}
	return cands[0].Token, cands[0].Distance
}

// Size returns the total count of registered tokens in the dictionary.
func (td *TokenDictionary) Size() int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return len(td.hvToToken)
}

// Contains checks whether a given token is present in the dictionary.
func (td *TokenDictionary) Contains(token string) bool {
	td.mu.RLock()
	defer td.mu.RUnlock()
	_, exists := td.tokenToHV[token]
	return exists
}

// GetAllTokens returns a slice of all registered token strings.
func (td *TokenDictionary) GetAllTokens() []string {
	td.mu.RLock()
	defer td.mu.RUnlock()

	tokens := make([]string, len(td.hvToToken))
	for i, pair := range td.hvToToken {
		tokens[i] = pair.Token
	}
	return tokens
}
