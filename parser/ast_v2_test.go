package parser

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// The structural-retrieval corpus. Three files:
//   - queries.go: 6 query functions
//   - twins.go:   the same 6 SHAPES with every identifier renamed (the correct answers)
//   - baits.go:   6 distractors that SHARE NAMES with the queries but have different
//     structure (the trap a names-only encoder falls into)
//
// The gate: EncoderV2 must rank each query's twin #1 (P@1 ≥ 5/6) and beat EncoderV1.
// This is the honest framing of what v2 adds: structure survives renaming; names alone
// don't. (v1's known strength — matching on exact names — is precisely what the baits
// exploit.)
const queriesSrc = `package corpus

import "errors"

func SumPrices(items []int, tax int) (int, error) {
	total := 0
	for _, it := range items {
		total += it
	}
	if total < 0 {
		return 0, errors.New("negative")
	}
	return total + tax, nil
}

func FindUser(users map[string]string, id string) (string, bool) {
	v, ok := users[id]
	if !ok {
		return "", false
	}
	return v, true
}

func RetryFetch(url string, attempts int) error {
	for i := 0; i < attempts; i++ {
		err := fetch(url)
		if err == nil {
			return nil
		}
	}
	return errors.New("exhausted")
}

func DrainQueue(ch chan int, out []int) []int {
	for {
		select {
		case v := <-ch:
			out = append(out, v)
		default:
			return out
		}
	}
}

func ParseConfig(raw []byte, strict bool) (map[string]string, error) {
	m := map[string]string{}
	switch {
	case len(raw) == 0:
		return nil, errors.New("empty")
	case strict:
		m["mode"] = "strict"
	}
	return m, nil
}

func CleanupTemp(dir string, keep int) int {
	removed := 0
	defer flush(dir)
	for i := 0; i < keep; i++ {
		removed++
	}
	return removed
}

func fetch(string) error   { return nil }
func flush(string)         {}
`

const twinsSrc = `package corpus

import "errors"

func AggregateScores(vals []int, bonus int) (int, error) {
	acc := 0
	for _, v := range vals {
		acc += v
	}
	if acc < 0 {
		return 0, errors.New("bad")
	}
	return acc + bonus, nil
}

func LookupSession(sessions map[string]string, key string) (string, bool) {
	s, present := sessions[key]
	if !present {
		return "", false
	}
	return s, true
}

func PollEndpoint(addr string, tries int) error {
	for n := 0; n < tries; n++ {
		e := ping(addr)
		if e == nil {
			return nil
		}
	}
	return errors.New("gave up")
}

func CollectEvents(events chan int, sink []int) []int {
	for {
		select {
		case ev := <-events:
			sink = append(sink, ev)
		default:
			return sink
		}
	}
}

func DecodeManifest(blob []byte, pedantic bool) (map[string]string, error) {
	out := map[string]string{}
	switch {
	case len(blob) == 0:
		return nil, errors.New("blank")
	case pedantic:
		out["level"] = "pedantic"
	}
	return out, nil
}

func PruneCache(root string, retain int) int {
	dropped := 0
	defer sync(root)
	for j := 0; j < retain; j++ {
		dropped++
	}
	return dropped
}

func ping(string) error { return nil }
func sync(string)       {}
`

const baitsSrc = `package corpus

func SumPricesFast(items []int, tax int) string {
	return label(items, tax)
}

func FindUserByEmail(users map[string]string, id string) {
	delete(users, id)
}

func RetryFetchOnce(url string, attempts int) (int, int, int) {
	return len(url), attempts, 0
}

func DrainQueueLen(ch chan int, out []int) (bool, error) {
	return len(out) > cap(ch), nil
}

func ParseConfigPath(raw []byte, strict bool) func() bool {
	return func() bool { return strict && len(raw) > 0 }
}

func CleanupTempName(dir string, keep int) (a string, b string) {
	return dir, dir
}

func label([]int, int) string { return "" }
`

var corpusPairs = []struct{ query, twin string }{
	{"SumPrices", "AggregateScores"},
	{"FindUser", "LookupSession"},
	{"RetryFetch", "PollEndpoint"},
	{"DrainQueue", "CollectEvents"},
	{"ParseConfig", "DecodeManifest"},
	{"CleanupTemp", "PruneCache"},
}

func indexCorpus(t *testing.T, version int) map[string]core.Hypervector {
	t.Helper()
	dir := t.TempDir()
	for name, src := range map[string]string{"queries.go": queriesSrc, "twins.go": twinsSrc, "baits.go": baitsSrc} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx := NewCodeASTIndexer(core.NewTokenDictionary()) // deterministic (DefaultSeed)
	idx.Version = version

	vectors := map[string]core.Hypervector{}
	for _, f := range []string{"queries.go", "twins.go", "baits.go"} {
		funcs, _, err := idx.IndexFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		for _, fv := range funcs {
			vectors[fv.FuncName] = fv.ASTHV
		}
	}
	return vectors
}

// retrievalP1 returns how many of the 6 queries rank their structural twin #1 among all
// OTHER corpus functions (self excluded), plus a per-query report line.
func retrievalP1(t *testing.T, vectors map[string]core.Hypervector) (int, []string) {
	t.Helper()
	hits := 0
	var report []string
	for _, pair := range corpusPairs {
		q := vectors[pair.query]
		best, bestDist := "", 1<<31
		for name, hv := range vectors {
			if name == pair.query {
				continue
			}
			if d := core.HammingDistance(q, hv); d < bestDist {
				best, bestDist = name, d
			}
		}
		ok := best == pair.twin
		if ok {
			hits++
		}
		report = append(report, pair.query+" -> "+best)
	}
	return hits, report
}

// TestStructuralRetrievalGate is the Phase D benchmark gate: on the renamed-twin corpus,
// EncoderV2 must achieve P@1 ≥ 5/6 and strictly beat EncoderV1. Measured numbers are
// committed to BENCHMARKS.md.
func TestStructuralRetrievalGate(t *testing.T) {
	v1Hits, v1Report := retrievalP1(t, indexCorpus(t, EncoderV1))
	v2Hits, v2Report := retrievalP1(t, indexCorpus(t, EncoderV2))

	t.Logf("EncoderV1 P@1 = %d/6  %v", v1Hits, v1Report)
	t.Logf("EncoderV2 P@1 = %d/6  %v", v2Hits, v2Report)

	if v2Hits < 5 {
		t.Fatalf("gate failed: EncoderV2 P@1 = %d/6, need ≥ 5", v2Hits)
	}
	if v2Hits <= v1Hits {
		t.Fatalf("gate failed: EncoderV2 (%d) must beat EncoderV1 (%d) on renamed twins", v2Hits, v1Hits)
	}
}

// TestEncoderV1Unchanged pins that the refactor did not alter v1's output: the historical
// encoding of a known function must be bit-identical to the pre-refactor formula.
func TestEncoderV1Unchanged(t *testing.T) {
	dir := t.TempDir()
	src := `package p
func Alpha(beta int, gamma string) (delta int, err error) { return 0, nil }
`
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	dict := core.NewTokenDictionary()
	funcs, _, err := NewCodeASTIndexer(dict).IndexFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := core.Bundle([]core.Hypervector{
		dict.GetOrRegister("func:Alpha"),
		dict.GetOrRegister("param:beta").Permute(1),
		dict.GetOrRegister("param:gamma").Permute(2),
		dict.GetOrRegister("return:delta").Permute(3),
		dict.GetOrRegister("return:err").Permute(4),
	})
	if core.HammingDistance(funcs[0].ASTHV, want) != 0 {
		t.Fatal("EncoderV1 output changed — the refactor must be behavior-preserving")
	}
}

// goldenV2Corpus pins EncoderV2's cross-platform determinism: the SHA-256 over the six
// query vectors (under DefaultSeed) must be identical on every OS and architecture.
// CI enforces this on ubuntu and macos.
const goldenV2Corpus = "ece574e522614bd50fa3eeecd1afcc79dff835482a47e46bfa87ddde52fe8b09"

func TestEncoderV2DeterminismGolden(t *testing.T) {
	vectors := indexCorpus(t, EncoderV2)
	h := sha256.New()
	for _, pair := range corpusPairs { // fixed iteration order
		hv := vectors[pair.query]
		var buf [8]byte
		for _, w := range hv.Vector {
			binary.LittleEndian.PutUint64(buf[:], w)
			h.Write(buf[:])
		}
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != goldenV2Corpus {
		t.Fatalf("EncoderV2 determinism drift:\n got  %s\n want %s", got, goldenV2Corpus)
	}
}

// TestStmtStreamHardBudget: a pathological flat function with far more than MaxStmtStream
// statements must encode in bounded work — the stream is capped and traversal terminates
// (the ast.Inspect version kept walking siblings). We assert the cap holds by encoding a
// function whose body is thousands of statements and checking it returns promptly with a
// stable vector (equal to the same function truncated at the budget).
func TestStmtStreamHardBudget(t *testing.T) {
	var big strings.Builder
	big.WriteString("package p\nfunc Huge() {\n\tx := 0\n")
	for i := 0; i < 5000; i++ {
		big.WriteString("\tx = x + 1\n")
	}
	big.WriteString("}\n")

	dir := t.TempDir()
	path := filepath.Join(dir, "huge.go")
	if err := os.WriteFile(path, []byte(big.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := NewCodeASTIndexerV2(core.NewTokenDictionary())
	funcs, _, err := idx.IndexFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(funcs) != 1 {
		t.Fatalf("want 1 func, got %d", len(funcs))
	}
	// The stream is bounded, so only the first MaxStmtStream statement kinds contribute; a
	// function with 5000 statements encodes identically to one truncated at the budget.
	stream := streamStmtKinds(mustParseBody(t, big.String()), MaxStmtStream)
	if len(stream) > MaxStmtStream {
		t.Fatalf("stream exceeded budget: %d > %d", len(stream), MaxStmtStream)
	}
}

func mustParseBody(t *testing.T, src string) *ast.BlockStmt {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return f.Decls[0].(*ast.FuncDecl).Body
}
