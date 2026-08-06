package arena

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Standard-dataset support (arena v2): CLINC150 and Banking77, fetched and converted by
// datasets/fetch.py (pinned by SHA-256, licenses in that file). Unlike the curated corpus
// there is no canonical/paraphrase split — just the official train/test — so results are
// reported as plain test accuracy plus macro-F1 (the class count makes accuracy alone too
// forgiving of minority-class failure).

// StandardIntent is one class of a standard dataset: official train and test utterances.
type StandardIntent struct {
	Name  string   `json:"name"`
	Train []string `json:"train"`
	Test  []string `json:"test"`
}

// StandardDataset is a converted standard corpus (see datasets/fetch.py for provenance).
type StandardDataset struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	LicenseNote string           `json:"license_note"`
	Intents     []StandardIntent `json:"intents"`
	// OOSTest holds out-of-scope test utterances (CLINC150 only): queries no trained
	// intent should claim. Scored separately via AUROC over the min-centroid distance.
	OOSTest []string `json:"oos_test,omitempty"`
}

// LoadStandardDataset reads a converted .arena.json produced by datasets/fetch.py.
func LoadStandardDataset(path string) (*StandardDataset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ds StandardDataset
	if err := json.Unmarshal(raw, &ds); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(ds.Intents) == 0 {
		return nil, fmt.Errorf("%s: no intents", path)
	}
	return &ds, nil
}

// MacroF1 computes the unweighted mean of per-class F1 scores. pred and gold are parallel
// slices of class names; classes is the full label set (a class with no predictions and no
// gold occurrences contributes F1 = 0 only if it appears in classes and in gold — classes
// absent from gold are skipped, matching the usual macro-F1-over-test-support convention).
func MacroF1(gold, pred []string, classes []string) float64 {
	if len(gold) != len(pred) {
		panic("MacroF1: gold and pred length mismatch")
	}
	tp := make(map[string]int)
	fp := make(map[string]int)
	fn := make(map[string]int)
	support := make(map[string]int)
	for i := range gold {
		support[gold[i]]++
		if gold[i] == pred[i] {
			tp[gold[i]]++
		} else {
			fp[pred[i]]++
			fn[gold[i]]++
		}
	}
	var sum float64
	var n int
	for _, c := range classes {
		if support[c] == 0 {
			continue
		}
		n++
		denomP := tp[c] + fp[c]
		denomR := tp[c] + fn[c]
		if denomP == 0 || denomR == 0 || tp[c] == 0 {
			continue // F1 = 0
		}
		p := float64(tp[c]) / float64(denomP)
		r := float64(tp[c]) / float64(denomR)
		sum += 2 * p * r / (p + r)
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// AUROC computes the area under the ROC curve for a score that should be HIGHER for
// positives (rank-based, ties get half credit). Used for OOS detection with
// score = min-centroid Hamming distance: an out-of-scope query should sit FARTHER from
// every prototype than an in-scope one.
func AUROC(posScores, negScores []float64) float64 {
	if len(posScores) == 0 || len(negScores) == 0 {
		return 0
	}
	type pt struct {
		s   float64
		pos bool
	}
	all := make([]pt, 0, len(posScores)+len(negScores))
	for _, s := range posScores {
		all = append(all, pt{s, true})
	}
	for _, s := range negScores {
		all = append(all, pt{s, false})
	}
	sort.Slice(all, func(a, b int) bool { return all[a].s < all[b].s })

	// Sum positive ranks with average ranks over ties (Mann–Whitney U).
	var rankSumPos float64
	i := 0
	for i < len(all) {
		j := i
		for j < len(all) && all[j].s == all[i].s {
			j++
		}
		avgRank := float64(i+1+j) / 2 // ranks are 1-based: mean of i+1 .. j
		for k := i; k < j; k++ {
			if all[k].pos {
				rankSumPos += avgRank
			}
		}
		i = j
	}
	nPos, nNeg := float64(len(posScores)), float64(len(negScores))
	u := rankSumPos - nPos*(nPos+1)/2
	return u / (nPos * nNeg)
}
