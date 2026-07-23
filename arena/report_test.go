package arena

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func readResults(path string) (results, error) {
	var r results
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(b, &r)
}

func pct(x float64) string { return fmt.Sprintf("%.1f%%", 100*x) }

func lowerWins(a, b float64, unit string) (string, string, string) {
	w := "tie"
	if a < b {
		w = "HDC"
	} else if b < a {
		w = "Neural"
	}
	return fmt.Sprintf("%.1f %s", a, unit), fmt.Sprintf("%.1f %s", b, unit), w
}

func higherWins(a, b float64) (string, string, string) {
	w := "tie"
	if a > b {
		w = "HDC"
	} else if b > a {
		w = "Neural"
	}
	return pct(a), pct(b), w
}

// TestArenaReport merges results_hdc.json + results_neural.json into ARENA_RESULTS.md.
// It is skipped (not failed) if either side has not been run yet.
func TestArenaReport(t *testing.T) {
	hdc, err := readResults("results_hdc.json")
	if err != nil {
		t.Skipf("no HDC results (run: go test -run TestArenaHDC): %v", err)
	}
	neu, err := readResults("results_neural.json")
	if err != nil {
		t.Skipf("no neural results (run: python3 neural_baseline.py): %v", err)
	}

	canH, canN, canW := higherWins(hdc.Accuracy.Canonical, neu.Accuracy.Canonical)
	parH, parN, parW := higherWins(hdc.Accuracy.Paraphrase, neu.Accuracy.Paraphrase)
	l50H, l50N, l50W := lowerWins(hdc.Latency.P50Us, neu.Latency.P50Us, "µs")
	l99H, l99N, l99W := lowerWins(hdc.Latency.P99Us, neu.Latency.P99Us, "µs")
	caH, caN, caW := lowerWins(hdc.ColdAdd.AddLatencyUs, neu.ColdAdd.AddLatencyUs, "µs")

	md := fmt.Sprintf(`# Structured-Routing Arena — Results

Same dataset (%d intents), same nearest-centroid routing algorithm. The **only** difference is
the representation: NeuroVSA HDC hypervectors vs. neural embeddings (`+"`%s`"+`). Both measured
on the same machine. Regenerate with the steps in [README.md](README.md).

| Axis | NeuroVSA (HDC) | %s | Winner |
| :--- | :--- | :--- | :--- |
| Canonical accuracy | %s | %s | %s |
| Paraphrase accuracy | %s | %s | %s |
| Latency p50 (encode+route) | %s | %s | %s |
| Latency p99 | %s | %s | %s |
| Cold-add latency | %s | %s | %s |
| Cold-add accuracy (after) | %s | %s | %s |
| Bit-exact & portable prototypes | yes | no | **HDC** |
| Within-run route determinism | %d mismatches | %d mismatches | tie |
| Model artifact required | none (pure algorithm) | yes (downloaded model) | **HDC** |

**Reading it:** both are perfect on canonical/in-grammar phrasing. The neural embedding wins
paraphrase (semantic generalization), and — because this static CPU embedding is just a token
lookup + mean-pool — it also wins latency and cold-add here. HDC's clear wins are integer-exact,
cross-machine-reproducible prototypes and zero model artifact. The honest crossover: HDC is
competitive only where inputs are bounded/canonical and the value is determinism/auditability/
no-dependency deployment — not where free-form paraphrase or raw speed decide it.
`,
		hdc.Classes, neu.Router, neu.Router,
		canH, canN, canW,
		parH, parN, parW,
		l50H, l50N, l50W,
		l99H, l99N, l99W,
		caH, caN, caW,
		pct(hdc.ColdAdd.AccAfter), pct(neu.ColdAdd.AccAfter), ternary(hdc.ColdAdd.AccAfter, neu.ColdAdd.AccAfter),
		hdc.Determinism.RouteMismatches, neu.Determinism.RouteMismatches,
	)

	if err := os.WriteFile("ARENA_RESULTS.md", []byte(md), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	fmt.Print("\n" + md + "\n")
}

func ternary(a, b float64) string {
	if a > b {
		return "HDC"
	} else if b > a {
		return "Neural"
	}
	return "tie"
}
