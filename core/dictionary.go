package core

import (
	"sync"
)

// TokenDictionary maintains a thread-safe mapping between crisp string tokens/AST identifiers
// and their assigned random base Hypervectors (Item Memory / Cleanup Memory).
type TokenDictionary struct {
	mu        sync.RWMutex
	tokenToHV map[string]Hypervector
	hvToToken []tokenPair
}

type tokenPair struct {
	Token string
	HV    Hypervector
}

// NewTokenDictionary initializes a clean TokenDictionary instance.
func NewTokenDictionary() *TokenDictionary {
	return &TokenDictionary{
		tokenToHV: make(map[string]Hypervector),
		hvToToken: make([]tokenPair, 0),
	}
}

// GetOrRegister retrieves the hypervector for a given token string.
// If the token is not yet registered, a new random hypervector is generated and stored.
func (td *TokenDictionary) GetOrRegister(token string) Hypervector {
	td.mu.Lock()
	defer td.mu.Unlock()

	if hv, exists := td.tokenToHV[token]; exists {
		return hv
	}

	newHV := GenerateRandom()
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
