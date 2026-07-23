// Package arena is a head-to-head benchmark ("the arena") pitting NeuroVSA's HDC router
// against a neural embedding router on structured intent routing. Both sides are prototype
// (nearest-centroid) classifiers over the same dataset; the only difference is the
// representation — HDC hypervectors vs. neural embeddings — which isolates exactly the
// variable under test.
package arena

import (
	_ "embed"
	"encoding/json"
)

//go:embed dataset.json
var datasetJSON []byte

// Intent is one routing class: training phrases plus two held-out test splits.
type Intent struct {
	Name           string   `json:"name"`
	Train          []string `json:"train"`
	TestCanonical  []string `json:"test_canonical"`  // same phrasing style as training
	TestParaphrase []string `json:"test_paraphrase"` // same meaning, different vocabulary
}

// Dataset is the full structured-routing corpus.
type Dataset struct {
	Description string   `json:"description"`
	Intents     []Intent `json:"intents"`
}

// LoadDataset parses the embedded dataset.json (identical bytes the Python baseline reads).
func LoadDataset() (*Dataset, error) {
	var ds Dataset
	if err := json.Unmarshal(datasetJSON, &ds); err != nil {
		return nil, err
	}
	return &ds, nil
}
