# NeuroVSA Developer Guide & Engineering Notes

## 1. Codebase Overview & Package Contracts

NeuroVSA is designed around modular, decoupled Go packages operating strictly on stack-allocated bit arrays without dynamic heap overhead inside critical inner loops.

```
core/        ──> Pure VSA Vector Physics & Math Primitives
parser/      ──> Language AST Tree-to-Hypervector Encoders
engine/      ──> Associative Memory, HDC Decoder, Agent Trajectory Router
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
- `engine.AssociativeMemory`: Stores sequence history and matrix associations with mmap/binary disk persistence.
- `engine.HDCDecoder`: Autoregressive sequence prediction loop.
- `engine.TrajectoryTracker`: Microsecond agent state tracker and tool selector ($51.79\,\mu\text{s}$ latency).

---

## 3. Extending the Codebase

### Adding New Agent Tools
To register a new tool action in the agent trajectory tracker (`engine/trajectory.go`):

1. Define the constant:
   ```go
   const ToolDatabaseQuery = "DatabaseQuery"
   ```
2. Add the tool to the registration list in `NewTrajectoryTracker()`:
   ```go
   tools := []string{ToolReadFile, ToolWriteFile, ToolRunTests, ToolASTSearch, ToolHTTPReq, ToolTerminal, ToolDatabaseQuery}
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
