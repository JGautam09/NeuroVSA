# NeuroVSA Developer Guide & Engineering Notes

## 1. Codebase Overview & Package Contracts

NeuroVSA uses fixed-size bit arrays for vector operations and separate packages for memory, application interfaces, and benchmark tooling. See [architecture](architecture.md) for how they fit together.

```
core/        ──> Pure VSA Vector Physics & Math Primitives
parser/      ──> Language AST Tree-to-Hypervector Encoders
engine/      ──> Associative memory, HDC decoder, trajectory router, ProofRoute
rulegarden/  ──> Headless world simulation; Go/WASM bridge in cmd/rulegarden-wasm/
web/rulegarden/ ──> Standalone RuleGarden page and browser checks
internal/checkselect/ ──> Policies, fixtures, signed packs, receipts, local MCP
cmd/nvsa-check/ ──> Check-selection CLI
api/         ──> Concurrent WebSocket Server & JSON Protocol Handlers
ui/          ──> React 18 + Tailwind CSS Streaming Frontend
```

---

## 2. Package Breakdown

### 2.1 Package `core`
The `core` package implements low-level vector algebra.

#### Important Symbols:
- `core.Hypervector`: The core 10,000-bit vector type (`struct { Vector [157]uint64 }`).
- `core.GenerateRandom() Hypervector`: Generates uniform binary vector with active bit masking on word 156.
- `hv.Bind(other Hypervector) Hypervector`: Bitwise XOR ($\otimes$).
- `hv.Permute(shift int) Hypervector`: Multi-word cyclic bit shift ($\rho$).
- `core.Bundle(vectors []Hypervector) Hypervector`: Bitwise Majority Vote ($\oplus$).
- `core.HammingDistance(a, b Hypervector) int`: CPU native POPCNT distance (`math/bits.OnesCount64`).
- `core.TokenDictionary`: Thread-safe item memory for symbol lookup and noise cleanup.

---

### 2.2 Package `parser`
The `parser` package extracts relational code structures.

#### Important Symbols:
- `parser.NewCodeASTIndexer(dict *core.TokenDictionary)`: Initializes AST indexer.
- `indexer.IndexFile(filePath string)`: Encodes single Go file AST into hypervector.
- `indexer.IndexDirectory(dirPath string)`: Recursively indexes target directory.

---

### 2.3 Package `engine`
The `engine` package manages sequence prediction memory and microsecond tool selection.

#### Important Symbols:
- `engine.AssociativeMemory`: Stores associations as a per-bit vote-counter vector (O(D) per write, independent of corpus size) plus a provenance **ledger** (bound vector + label per association). `StoreLabeled` returns an `AssociationID`; `RemoveAssociation(id)` exactly unlearns one association (O(D) counter decrement — bit-identical to never having stored it); `Contributors(probe, 0)` returns active ledger associations whose bound vector exactly matches the probe; `Ledger`/`FindByLabel` expose provenance. Memory-mapped (`mmap`) v2 persistence carries the vocab seed and the full ledger; `OpenReadOnly` uses a temporary mapping to load only the vocab seed and materialized matrix into a query-only object, skipping the tally and ledger.
- `engine.HDCDecoder`: Autoregressive sequence prediction loop. `EncodeContext` builds seed contexts with the same permute-bind recurrence used in training; `StopThreshold` halts generation at the noise floor. `PredictNextTokenTraced` / `GenerateSequenceTraced` return first-class `PredictionTrace`/`GenerationTrace` derivations (candidate tables, symbolic ops, stop reasons, contributors); the untraced methods delegate to them.
- `engine.ToolRouter` / `engine.TrajectoryTracker`: A shared learned routing policy plus per-agent trajectory state; goal- and history-dependent tool selection ($\approx 1.03\,\mu\text{s}$ latency). `SelectNextToolTraced` returns the ranked tool table and any exact-match workflow-step association(s) for the decision.
- `core.SeededHV(seed, token)` / `core.NewSeededTokenDictionary`: The deterministic item memory — token vectors are bit-identical across runs and machines (golden vectors under `core/testdata/` are verified by CI on ubuntu and macos). `core.NewRandomTokenDictionary` preserves the legacy crypto/rand behavior; `core.LookupCandidates` exposes the full ranked cleanup table.

---

## 3. Extending the Codebase

### Adding New Agent Tools
To register a new tool action and teach the router when to use it (`engine/trajectory.go`):

1. Define the constant and add it to the `StandardTools` cleanup vocabulary:
   ```go
   const ToolDatabaseQuery = "DatabaseQuery"

   var StandardTools = []string{
       ToolReadFile, ToolWriteFile, ToolRunTests, ToolASTSearch, ToolHTTPReq, ToolTerminal, ToolDatabaseQuery,
   }
   ```
2. Teach a workflow that uses it by adding to `DefaultWorkflows` (or call
   `router.RegisterWorkflow(goal, actions)` at runtime):
   ```go
   {Goal: "query_db", Actions: []string{ToolReadFile, ToolDatabaseQuery, ToolWriteFile}},
   ```

### Adding New VSA Operators
To implement a custom VSA operator (such as Fractional Shift or Resonator Unbinding), add the method directly to `core/hypervector.go`:

```go
// FractionalPermute shifts fractional bit blocks for multi-scale position encoding.
func (hv Hypervector) FractionalPermute(fraction float64) Hypervector {
    shift := int(fraction * float64(Dimension))
    return hv.Permute(shift)
}
```

---

## 4. Running Tests & Benchmarks

### Unit Tests
```bash
# Run all unit tests
go test -v ./...

# Run core tests with race detector
go test -v -race ./core/...
```

### Benchmarks
To benchmark hypervector operations per second:
```bash
go test -bench=. -benchmem ./...
```

### Check selection and RuleGarden

```sh
go test -race ./internal/checkselect ./cmd/nvsa-check
go run ./cmd/nvsa-check bench -corpus arena/check-selection/corpus.json -repeat 200
node web/rulegarden/security.test.mjs
sh web/rulegarden/build.sh
```

When changing a selector, add a labeled case with baseline and expected behavior.
Do not train HDC on holdout cases or change the fixed split to improve a result.
For adapter changes, verify that the same structured change and prepared snapshot
produce matching results. For a new signed field, update the versioned signing format
and add a verifier regression test. See the [benchmark report](../arena/check-selection/RESULTS.md)
and the [CLI and file-format reference](check-selection-replay.md).
