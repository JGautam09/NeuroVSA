package core

import (
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

// LookupToken performs a minimum Hamming distance search across clean dictionary entries
// to clean up a noisy query hypervector into its exact token string representation.
// Returns the token string and the minimum Hamming distance.
func (td *TokenDictionary) LookupToken(query Hypervector) (string, int) {
	td.mu.RLock()
	defer td.mu.RUnlock()

	if len(td.hvToToken) == 0 {
		return "", -1
	}

	bestToken := ""
	minDist := Dimension + 1

	for _, pair := range td.hvToToken {
		dist := HammingDistance(query, pair.HV)
		if dist < minDist {
			minDist = dist
			bestToken = pair.Token
		}
	}

	return bestToken, minDist
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
