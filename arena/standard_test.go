package arena

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ---- hermetic metric tests (no network, no dataset files) ----

func TestMacroF1(t *testing.T) {
	classes := []string{"a", "b", "c"}
	gold := []string{"a", "a", "b", "b", "c", "c"}
	pred := []string{"a", "a", "b", "b", "c", "c"}
	if got := MacroF1(gold, pred, classes); got != 1.0 {
		t.Fatalf("perfect predictions must score 1.0, got %v", got)
	}
	// Class a: tp=2 (p=2/3, r=1) f1=0.8. Class b: tp=1, fn=1 (p=1, r=0.5) f1=2/3.
	// Class c: tp=2 (p=1, r=1) f1=1. Macro = (0.8 + 2/3 + 1) / 3.
	pred = []string{"a", "a", "b", "a", "c", "c"}
	want := (0.8 + 2.0/3.0 + 1.0) / 3.0
	if got := MacroF1(gold, pred, classes); math.Abs(got-want) > 1e-12 {
		t.Fatalf("macro-F1 = %v, want %v", got, want)
	}
	// A class never predicted and never gold ("d") must not dilute the mean.
	if got := MacroF1(gold, pred, append(classes, "d")); math.Abs(got-want) > 1e-12 {
		t.Fatalf("zero-support class changed macro-F1: %v", got)
	}
}

func TestAUROC(t *testing.T) {
	// Perfect separation: every positive scores above every negative.
	if got := AUROC([]float64{3, 4, 5}, []float64{0, 1, 2}); got != 1.0 {
		t.Fatalf("perfect separation must be 1.0, got %v", got)
	}
	// Fully inverted → 0. Identical distributions → 0.5 (all ties).
	if got := AUROC([]float64{0, 1}, []float64{2, 3}); got != 0.0 {
		t.Fatalf("inverted must be 0.0, got %v", got)
	}
	if got := AUROC([]float64{1, 1}, []float64{1, 1}); got != 0.5 {
		t.Fatalf("all-ties must be 0.5, got %v", got)
	}
}

// ---- the gated standard-dataset run (fetch datasets first; see datasets/fetch.py) ----

type standardResults struct {
	Router       string  `json:"router"`
	Dataset      string  `json:"dataset"`
	Classes      int     `json:"classes"`
	TestTotal    int     `json:"test_total"`
	Accuracy     float64 `json:"accuracy"`
	MacroF1      float64 `json:"macro_f1"`
	P50Us        float64 `json:"p50_us"`
	P99Us        float64 `json:"p99_us"`
	TrainSeconds float64 `json:"train_seconds"`
	ColdAddUs    float64 `json:"cold_add_us"`
	ColdAddName  string  `json:"cold_add_intent"`
	ColdAddAcc   float64 `json:"cold_add_acc_after"`
	OOSAUROC     float64 `json:"oos_auroc,omitempty"`
	OOSTotal     int     `json:"oos_total,omitempty"`
}

// TestArenaHDCStandard runs the HDC router over every fetched standard dataset. Gated:
// plain `go test ./...` stays hermetic and network-free — set ARENA_STANDARD=1 after
// running datasets/fetch.py.
func TestArenaHDCStandard(t *testing.T) {
	if os.Getenv("ARENA_STANDARD") != "1" {
		t.Skip("standard-dataset run is opt-in: python3 arena/datasets/fetch.py && ARENA_STANDARD=1 go test ./arena -run TestArenaHDCStandard")
	}
	files, _ := filepath.Glob(filepath.Join("datasets", "*.arena.json"))
	if len(files) == 0 {
		t.Fatal("no datasets/*.arena.json found — run python3 arena/datasets/fetch.py first")
	}

	for _, f := range files {
		ds, err := LoadStandardDataset(f)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(ds.Name, func(t *testing.T) {
			res := runHDCStandard(t, ds)
			b, _ := json.MarshalIndent(res, "", "  ")
			out := fmt.Sprintf("results_hdc_%s.json", ds.Name)
			if err := os.WriteFile(out, b, 0o644); err != nil {
				t.Fatal(err)
			}
			fmt.Printf("=== %s (HDC): acc %.1f%%  macro-F1 %.3f  p50 %.1f µs  cold-add %.1f µs%s\n",
				ds.Name, 100*res.Accuracy, res.MacroF1, res.P50Us, res.ColdAddUs,
				map[bool]string{true: fmt.Sprintf("  oos-AUROC %.3f", res.OOSAUROC), false: ""}[res.OOSTotal > 0])
		})
	}
}

func runHDCStandard(t *testing.T, ds *StandardDataset) standardResults {
	t.Helper()

	intents := make([]Intent, len(ds.Intents))
	classes := make([]string, len(ds.Intents))
	for i, in := range ds.Intents {
		intents[i] = Intent{Name: in.Name, Train: in.Train}
		classes[i] = in.Name
	}

	c := NewClassifier()
	trainStart := time.Now()
	c.Train(intents)
	trainSecs := time.Since(trainStart).Seconds()

	// Accuracy + macro-F1 + per-query latency in one pass over the official test split.
	var gold, pred []string
	var durs []float64
	for _, in := range ds.Intents {
		for _, u := range in.Test {
			start := time.Now()
			got, _ := c.Route(u)
			durs = append(durs, float64(time.Since(start).Nanoseconds())/1000.0)
			gold = append(gold, in.Name)
			pred = append(pred, got)
		}
	}
	sort.Float64s(durs)
	correct := 0
	for i := range gold {
		if gold[i] == pred[i] {
			correct++
		}
	}

	res := standardResults{
		Router: "hdc", Dataset: ds.Name, Classes: len(ds.Intents), TestTotal: len(gold),
		Accuracy: float64(correct) / float64(len(gold)),
		MacroF1:  MacroF1(gold, pred, classes),
		P50Us:    percentile(durs, 0.50), P99Us: percentile(durs, 0.99),
		TrainSeconds: trainSecs,
	}

	// Cold-add: hold out the alphabetically first intent, inject at runtime, score its
	// own test phrases.
	held := ds.Intents[0]
	rest := make([]Intent, 0, len(intents)-1)
	for _, in := range intents {
		if in.Name != held.Name {
			rest = append(rest, in)
		}
	}
	cc := NewClassifier()
	cc.Train(rest)
	start := time.Now()
	cc.ColdAdd(held.Name, held.Train)
	res.ColdAddUs = float64(time.Since(start).Nanoseconds()) / 1000.0
	res.ColdAddName = held.Name
	hit := 0
	for _, u := range held.Test {
		if got, _ := cc.Route(u); got == held.Name {
			hit++
		}
	}
	if len(held.Test) > 0 {
		res.ColdAddAcc = float64(hit) / float64(len(held.Test))
	}

	// OOS detection (CLINC150): AUROC of min-centroid distance separating out-of-scope
	// from in-scope test queries — higher distance should mean out-of-scope.
	if len(ds.OOSTest) > 0 {
		inDist := make([]float64, 0, len(gold))
		for _, in := range ds.Intents {
			for _, u := range in.Test {
				_, d := c.Route(u)
				inDist = append(inDist, float64(d))
			}
		}
		oosDist := make([]float64, 0, len(ds.OOSTest))
		for _, u := range ds.OOSTest {
			_, d := c.Route(u)
			oosDist = append(oosDist, float64(d))
		}
		res.OOSAUROC = AUROC(oosDist, inDist) // positives = OOS (should score higher)
		res.OOSTotal = len(ds.OOSTest)
	}
	return res
}

// TestArenaStandardReport merges every available results_*_<dataset>.json into
// ARENA_RESULTS_STANDARD.md. Gated like the runner; skips when nothing has been run.
func TestArenaStandardReport(t *testing.T) {
	if os.Getenv("ARENA_STANDARD") != "1" {
		t.Skip("standard-dataset report is opt-in (ARENA_STANDARD=1)")
	}
	files, _ := filepath.Glob("results_*_*.json")
	byDataset := map[string][]standardResults{}
	var datasets []string
	for _, f := range files {
		var r standardResults
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if err := json.Unmarshal(raw, &r); err != nil || r.Dataset == "" || r.TestTotal == 0 {
			continue // not a standard-results file (e.g. the curated results)
		}
		if len(byDataset[r.Dataset]) == 0 {
			datasets = append(datasets, r.Dataset)
		}
		byDataset[r.Dataset] = append(byDataset[r.Dataset], r)
	}
	if len(datasets) == 0 {
		t.Skip("no standard results yet — run TestArenaHDCStandard and/or standard_baseline.py")
	}
	sort.Strings(datasets)

	var md strings.Builder
	md.WriteString(`# Arena v2 — Standard Datasets

Official train/test splits of CLINC150 (150 intents, + 1000 out-of-scope test queries)
and Banking77 (77 intents), fetched and converted by [datasets/fetch.py](datasets/fetch.py)
(SHA-256-pinned; licenses noted there). Same nearest-centroid protocol for every router —
only the representation differs. Regenerate: fetch, then ARENA_STANDARD=1 with
TestArenaHDCStandard (Go side), standard_baseline.py (Python side), and
TestArenaStandardReport.

`)
	for _, dsName := range datasets {
		rs := byDataset[dsName]
		sort.Slice(rs, func(a, b int) bool { return rs[a].Router < rs[b].Router })
		md.WriteString(fmt.Sprintf("## %s\n\n", dsName))
		md.WriteString("| Router | Accuracy | Macro-F1 | p50 µs | p99 µs | Cold-add µs | Cold-add acc | OOS AUROC |\n")
		md.WriteString("| :-- | --: | --: | --: | --: | --: | --: | --: |\n")
		for _, r := range rs {
			oos := "—"
			if r.OOSTotal > 0 {
				oos = fmt.Sprintf("%.3f", r.OOSAUROC)
			}
			md.WriteString(fmt.Sprintf("| %s | %.1f%% | %.3f | %.1f | %.1f | %.0f | %.1f%% | %s |\n",
				r.Router, 100*r.Accuracy, r.MacroF1, r.P50Us, r.P99Us, r.ColdAddUs, 100*r.ColdAddAcc, oos))
		}
		md.WriteString("\n")
	}
	md.WriteString(`Read it straight: routers appear only if their run completed on this
machine — a missing row is a missing measurement, never a hidden loss. Cross-router speed
comparisons are same-machine indicative; accuracy and macro-F1 are split-exact.
`)

	if err := os.WriteFile("ARENA_RESULTS_STANDARD.md", []byte(md.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Print("\n" + md.String() + "\n")
}
