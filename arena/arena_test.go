package arena

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"
)

// ---- shared result schema (the Python baseline emits the same shape) ----

type accuracyMetrics struct {
	Canonical         float64 `json:"canonical"`
	Paraphrase        float64 `json:"paraphrase"`
	CanonicalCorrect  int     `json:"canonical_correct"`
	CanonicalTotal    int     `json:"canonical_total"`
	ParaphraseCorrect int     `json:"paraphrase_correct"`
	ParaphraseTotal   int     `json:"paraphrase_total"`
}

type latencyMetrics struct {
	P50Us   float64 `json:"p50_us"`
	P99Us   float64 `json:"p99_us"`
	MeanUs  float64 `json:"mean_us"`
	Samples int     `json:"samples"`
}

type determinismMetrics struct {
	RunsCompared           int  `json:"runs_compared"`
	RouteMismatches        int  `json:"route_mismatches"`
	BitIdenticalPrototypes bool `json:"bit_identical_prototypes"`
}

type coldAddMetrics struct {
	HeldOutIntent string  `json:"held_out_intent"`
	AddLatencyUs  float64 `json:"add_latency_us"`
	AccBefore     float64 `json:"acc_before"`
	AccAfter      float64 `json:"acc_after"`
	TestPhrases   int     `json:"test_phrases"`
}

type results struct {
	Router      string             `json:"router"`
	Dataset     string             `json:"dataset"`
	Classes     int                `json:"classes"`
	Accuracy    accuracyMetrics    `json:"accuracy"`
	Latency     latencyMetrics     `json:"latency"`
	Determinism determinismMetrics `json:"determinism"`
	ColdAdd     coldAddMetrics     `json:"cold_add"`
}

func accuracyOn(c *Classifier, intents []Intent, pick func(Intent) []string) (int, int) {
	correct, total := 0, 0
	for _, in := range intents {
		for _, u := range pick(in) {
			total++
			if got, _ := c.Route(u); got == in.Name {
				correct++
			}
		}
	}
	return correct, total
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(p*float64(len(sorted)-1))]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// TestArenaHDC measures the HDC router across all four axes and writes results_hdc.json.
func TestArenaHDC(t *testing.T) {
	ds, err := LoadDataset()
	if err != nil {
		t.Fatal(err)
	}

	c := NewClassifier()
	c.Train(ds.Intents)

	// --- Axis 1: accuracy, split canonical vs paraphrase ---
	cc, ct := accuracyOn(c, ds.Intents, func(in Intent) []string { return in.TestCanonical })
	pc, pt := accuracyOn(c, ds.Intents, func(in Intent) []string { return in.TestParaphrase })

	// --- Axis 2: latency (encode + route per query, matching the neural side) ---
	var probes []string
	for _, in := range ds.Intents {
		probes = append(probes, in.TestCanonical...)
		probes = append(probes, in.TestParaphrase...)
	}
	durs := make([]float64, 0, len(probes)*50)
	for r := 0; r < 50; r++ {
		for _, u := range probes {
			start := time.Now()
			c.Route(u)
			durs = append(durs, float64(time.Since(start).Nanoseconds())/1000.0)
		}
	}
	sort.Float64s(durs)

	// --- Axis 3: determinism (an independently built classifier must be bit-identical) ---
	c2 := NewClassifier()
	c2.Train(ds.Intents)
	mismatches := 0
	for _, u := range probes {
		g1, d1 := c.Route(u)
		g2, d2 := c2.Route(u)
		if g1 != g2 || d1 != d2 {
			mismatches++
		}
	}
	bitIdentical := true
	for _, in := range ds.Intents {
		if c.prototypes[in.Name] != c2.prototypes[in.Name] {
			bitIdentical = false
			break
		}
	}

	// --- Axis 4: cold-add (hold out one intent, inject it at runtime) ---
	const heldOut = "add_todo"
	var trainSet []Intent
	var held Intent
	for _, in := range ds.Intents {
		if in.Name == heldOut {
			held = in
			continue
		}
		trainSet = append(trainSet, in)
	}
	cca := NewClassifier()
	cca.Train(trainSet)
	heldTests := append(append([]string{}, held.TestCanonical...), held.TestParaphrase...)

	before := 0
	for _, u := range heldTests {
		if got, _ := cca.Route(u); got == heldOut {
			before++
		}
	}
	start := time.Now()
	cca.ColdAdd(heldOut, held.Train)
	addUs := float64(time.Since(start).Nanoseconds()) / 1000.0
	after := 0
	for _, u := range heldTests {
		if got, _ := cca.Route(u); got == heldOut {
			after++
		}
	}

	res := results{
		Router:  "hdc",
		Dataset: "structured-routing-v1",
		Classes: len(ds.Intents),
		Accuracy: accuracyMetrics{
			Canonical: float64(cc) / float64(ct), Paraphrase: float64(pc) / float64(pt),
			CanonicalCorrect: cc, CanonicalTotal: ct, ParaphraseCorrect: pc, ParaphraseTotal: pt,
		},
		Latency: latencyMetrics{
			P50Us: percentile(durs, 0.50), P99Us: percentile(durs, 0.99),
			MeanUs: mean(durs), Samples: len(durs),
		},
		Determinism: determinismMetrics{RunsCompared: 2, RouteMismatches: mismatches, BitIdenticalPrototypes: bitIdentical},
		ColdAdd: coldAddMetrics{
			HeldOutIntent: heldOut, AddLatencyUs: addUs,
			AccBefore:   float64(before) / float64(len(heldTests)),
			AccAfter:    float64(after) / float64(len(heldTests)),
			TestPhrases: len(heldTests),
		},
	}

	b, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile("results_hdc.json", b, 0o644); err != nil {
		t.Fatalf("write results: %v", err)
	}

	fmt.Printf("\n=== NeuroVSA HDC Router — Structured Routing Arena ===\n")
	fmt.Printf("Classes: %d | Dataset: %s\n", res.Classes, res.Dataset)
	fmt.Printf("Accuracy    canonical: %.1f%% (%d/%d)   paraphrase: %.1f%% (%d/%d)\n",
		100*res.Accuracy.Canonical, cc, ct, 100*res.Accuracy.Paraphrase, pc, pt)
	fmt.Printf("Latency     p50: %.2f µs   p99: %.2f µs   mean: %.2f µs (n=%d)\n",
		res.Latency.P50Us, res.Latency.P99Us, res.Latency.MeanUs, res.Latency.Samples)
	fmt.Printf("Determinism route mismatches across 2 independent builds: %d   prototypes bit-identical: %v\n",
		res.Determinism.RouteMismatches, res.Determinism.BitIdenticalPrototypes)
	fmt.Printf("Cold-add    %q: add %.1f µs   acc before: %.0f%%   after: %.0f%% (%d phrases)\n\n",
		heldOut, res.ColdAdd.AddLatencyUs, 100*res.ColdAdd.AccBefore, 100*res.ColdAdd.AccAfter, res.ColdAdd.TestPhrases)

	if mismatches != 0 || !bitIdentical {
		t.Errorf("determinism violated: mismatches=%d bitIdentical=%v", mismatches, bitIdentical)
	}
}

// BenchmarkRoute is a precise per-query latency measurement (encode + nearest-centroid).
func BenchmarkRoute(b *testing.B) {
	ds, _ := LoadDataset()
	c := NewClassifier()
	c.Train(ds.Intents)
	u := ds.Intents[0].TestCanonical[0]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Route(u)
	}
}
