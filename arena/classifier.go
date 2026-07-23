package arena

import (
	"sort"

	"github.com/JGautam09/NeuroVSA/core"
)

// Classifier is a prototype (nearest-centroid) router over HDC utterance vectors. Each class
// prototype is the bundle of its training utterance vectors; routing returns the minimum-
// Hamming class. This mirrors the neural baseline exactly — same algorithm, different
// representation.
type Classifier struct {
	enc        *Encoder
	names      []string
	prototypes map[string]core.Hypervector
}

// NewClassifier returns an empty classifier with a deterministic encoder.
func NewClassifier() *Classifier {
	return &Classifier{
		enc:        NewEncoder(),
		prototypes: make(map[string]core.Hypervector),
	}
}

// Train builds a prototype for every intent from its training utterances.
func (c *Classifier) Train(intents []Intent) {
	for _, in := range intents {
		c.addClass(in.Name, in.Train)
	}
}

func (c *Classifier) addClass(name string, examples []string) {
	vecs := make([]core.Hypervector, 0, len(examples))
	for _, u := range examples {
		vecs = append(vecs, c.enc.Encode(u))
	}
	c.prototypes[name] = core.Bundle(vecs)
	c.names = append(c.names, name)
	sort.Strings(c.names) // keep class order deterministic for tie-breaking
}

// ColdAdd injects a brand-new intent at runtime from k examples. It is O(k · D): no
// retraining and no re-encoding of existing classes. Time it externally for the cold-add axis.
func (c *Classifier) ColdAdd(name string, examples []string) {
	c.addClass(name, examples)
}

// Route returns the nearest class and its Hamming distance. The candidate list is kept sorted,
// and ties are broken by class-name order, so routing is fully deterministic.
func (c *Classifier) Route(utterance string) (string, int) {
	q := c.enc.Encode(utterance)
	best := ""
	bestDist := core.Dimension + 1
	for _, name := range c.names {
		if d := core.HammingDistance(q, c.prototypes[name]); d < bestDist {
			bestDist = d
			best = name
		}
	}
	return best, bestDist
}
