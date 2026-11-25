package arena

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
)

// The dimensionality study (the last original-roadmap item): measure what 10,000 bits
// actually buys against 4,096 / 2,048 / 1,024. One gated test emits a per-dimension JSON;
// scripts/dimscan.sh runs it under each hd_d* build tag; TestDimScanReport merges the
// points into docs/DIMENSIONALITY.md. Nothing here runs in a plain `go test ./...`.

type dimCapacityPoint struct {
	K         int     `json:"k"`
	Recall    float64 `json:"recall"`
	MinMargin int     `json:"min_margin"`
}

type dimPoint struct {
	Dimension      int                `json:"dimension"`
	NumWords       int                `json:"num_words"`
	BytesPerVector int                `json:"bytes_per_vector"`
	BindNs         int64              `json:"bind_ns"`
	HammingNs      int64              `json:"hamming_ns"`
	PermuteNs      int64              `json:"permute_ns"`
	Bundle8Ns      int64              `json:"bundle8_ns"`
	CuratedCanon   float64            `json:"curated_canonical_acc"`
	CuratedPara    float64            `json:"curated_paraphrase_acc"`
	Capacity       []dimCapacityPoint `json:"capacity"`
	StandardAcc    map[string]float64 `json:"standard_acc,omitempty"`
}

// TestDimensionPoint measures this build's dimension. Run via scripts/dimscan.sh.
func TestDimensionPoint(t *testing.T) {
	if os.Getenv("DIMSCAN") != "1" {
		t.Skip("dimensionality-study point is opt-in: see scripts/dimscan.sh")
	}

	p := dimPoint{
		Dimension:      core.Dimension,
		NumWords:       core.NumWords,
		BytesPerVector: core.NumWords * 8,
	}

	// Core op latencies, measured the same way `go test -bench` would.
	a, b := core.SeededHV(1, "dim-a"), core.SeededHV(1, "dim-b")
	p.BindNs = int64(testing.Benchmark(func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			_ = a.Bind(b)
		}
	}).NsPerOp())
	p.HammingNs = int64(testing.Benchmark(func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			_ = core.HammingDistance(a, b)
		}
	}).NsPerOp())
	p.PermuteNs = int64(testing.Benchmark(func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			_ = a.Permute(1)
		}
	}).NsPerOp())
	eight := make([]core.Hypervector, 8)
	for i := range eight {
		eight[i] = core.SeededHV(2, fmt.Sprintf("dim-b8-%d", i))
	}
	p.Bundle8Ns = int64(testing.Benchmark(func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			_ = core.Bundle(eight)
		}
	}).NsPerOp())

	// Curated arena accuracy (canonical + paraphrase).
	ds, err := LoadDataset()
	if err != nil {
		t.Fatal(err)
	}
	c := NewClassifier()
	c.Train(ds.Intents)
	cc, ct := accuracyOn(c, ds.Intents, func(in Intent) []string { return in.TestCanonical })
	pc, pt := accuracyOn(c, ds.Intents, func(in Intent) []string { return in.TestParaphrase })
	p.CuratedCanon = float64(cc) / float64(ct)
	p.CuratedPara = float64(pc) / float64(pt)

	// Capacity: the G0 random-context regime — K (context → one-of-4-actions) pairs in one
	// associative memory, cleanup over the 4 actions; report recall and the worst margin.
	dict := core.NewSeededTokenDictionary(99)
	actions := make([]core.Hypervector, 4)
	for i := range actions {
		actions[i] = dict.GetOrRegister(fmt.Sprintf("dim-action-%d", i))
	}
	for _, k := range []int{16, 32, 64, 128, 256, 512} {
		mem := engine.NewAssociativeMemory()
		mem.SetVocabSeed(99)
		ctxs := make([]core.Hypervector, k)
		for i := 0; i < k; i++ {
			ctxs[i] = dict.GetOrRegister(fmt.Sprintf("dim-ctx-%d", i))
			mem.StoreAssociation(ctxs[i], actions[i%4])
		}
		correct, minMargin := 0, core.Dimension
		for i := 0; i < k; i++ {
			q := mem.Matrix().Bind(ctxs[i])
			best, second, bestIdx := core.Dimension+1, core.Dimension+1, -1
			for ai, av := range actions {
				d := core.HammingDistance(q, av)
				if d < best {
					second, best, bestIdx = best, d, ai
				} else if d < second {
					second = d
				}
			}
			if bestIdx == i%4 {
				correct++
			}
			if m := second - best; m < minMargin {
				minMargin = m
			}
		}
		p.Capacity = append(p.Capacity, dimCapacityPoint{
			K: k, Recall: float64(correct) / float64(k), MinMargin: minMargin,
		})
	}

	// Standard datasets, when fetched (accuracy only — the axis dimension affects).
	if files, _ := filepath.Glob(filepath.Join("datasets", "*.arena.json")); len(files) > 0 {
		p.StandardAcc = map[string]float64{}
		for _, f := range files {
			sd, err := LoadStandardDataset(f)
			if err != nil {
				t.Fatal(err)
			}
			intents := make([]Intent, len(sd.Intents))
			for i, in := range sd.Intents {
				intents[i] = Intent{Name: in.Name, Train: in.Train}
			}
			sc := NewClassifier()
			sc.Train(intents)
			correct, total := 0, 0
			for _, in := range sd.Intents {
				for _, u := range in.Test {
					total++
					if got, _ := sc.Route(u); got == in.Name {
						correct++
					}
				}
			}
			p.StandardAcc[sd.Name] = float64(correct) / float64(total)
		}
	}

	out := fmt.Sprintf("dimscan_%d.json", core.Dimension)
	raw, _ := json.MarshalIndent(p, "", "  ")
	if err := os.WriteFile(out, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("=== D=%d: bind %dns  bundle8 %dns  curated %s/%s  standard %v\n",
		p.Dimension, p.BindNs, p.Bundle8Ns,
		pctf(p.CuratedCanon), pctf(p.CuratedPara), p.StandardAcc)
}

func pctf(v float64) string { return fmt.Sprintf("%.1f%%", 100*v) }

// TestDimScanReport merges dimscan_*.json points into docs/DIMENSIONALITY.md. Run it from
// the DEFAULT build after the tag sweep (scripts/dimscan.sh does).
func TestDimScanReport(t *testing.T) {
	if os.Getenv("DIMSCAN") != "1" {
		t.Skip("dimensionality-study report is opt-in (DIMSCAN=1)")
	}
	files, _ := filepath.Glob("dimscan_*.json")
	if len(files) == 0 {
		t.Skip("no dimscan points yet — run scripts/dimscan.sh")
	}
	var pts []dimPoint
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var p dimPoint
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		pts = append(pts, p)
	}
	sort.Slice(pts, func(a, b int) bool { return pts[a].Dimension < pts[b].Dimension })

	var md []byte
	add := func(s string, args ...any) { md = append(md, fmt.Sprintf(s, args...)...) }
	add(`# Dimensionality study

What do 10,000 bits buy over smaller hypervectors? Every number below is measured by
` + "`TestDimensionPoint`" + ` compiled at each dimension (` + "`scripts/dimscan.sh`" + `; build tags
` + "`hd_d1024`/`hd_d2048`/`hd_d4096`" + `), same machine, same seeds, same code. The default
build stays 10,000 — study builds are measurement instruments, not supported
configurations (goldens skip; file formats embed the dimension and refuse mismatches).

## Cost per operation

| D | bytes/vec | bind | Hamming | permute | Bundle8 |
| --: | --: | --: | --: | --: | --: |
`)
	for _, p := range pts {
		add("| %d | %d | %d ns | %d ns | %d ns | %d ns |\n",
			p.Dimension, p.BytesPerVector, p.BindNs, p.HammingNs, p.PermuteNs, p.Bundle8Ns)
	}

	add(`
## Routing accuracy vs dimension

| D | curated canonical | curated paraphrase |`)
	var stdNames []string
	if len(pts) > 0 {
		for name := range pts[len(pts)-1].StandardAcc {
			stdNames = append(stdNames, name)
		}
		sort.Strings(stdNames)
		for _, n := range stdNames {
			add(" %s |", n)
		}
	}
	add("\n| --: | --: | --: |")
	for range stdNames {
		add(" --: |")
	}
	add("\n")
	for _, p := range pts {
		add("| %d | %s | %s |", p.Dimension, pctf(p.CuratedCanon), pctf(p.CuratedPara))
		for _, n := range stdNames {
			if v, ok := p.StandardAcc[n]; ok {
				add(" %s |", pctf(v))
			} else {
				add(" — |")
			}
		}
		add("\n")
	}

	add(`
## Capacity (G0 random-context regime: K pairs, 4-action cleanup)

| D |`)
	if len(pts) > 0 {
		for _, cp := range pts[0].Capacity {
			add(" K=%d |", cp.K)
		}
	}
	add("\n| --: |")
	for range pts[0].Capacity {
		add(" --: |")
	}
	add("\n")
	for _, p := range pts {
		add("| %d |", p.Dimension)
		for _, cp := range p.Capacity {
			add(" %s (m≥%d) |", pctf(cp.Recall), cp.MinMargin)
		}
		add("\n")
	}

	add(`
Read it straight: accuracy and capacity columns state exactly what was measured — where a
smaller dimension holds recall, the extra bits were headroom for THAT load, not free
accuracy; where it collapses, that is the superposition floor arriving early. Per-op cost
scales with words (D/64), so the speed/size win of a smaller D is mechanical.
`)

	if err := os.WriteFile(filepath.Join("..", "docs", "DIMENSIONALITY.md"), md, 0o644); err != nil {
		t.Fatal(err)
	}
	fmt.Print("\n" + string(md) + "\n")
}
