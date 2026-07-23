# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] — 0.2.0 "Foundations"

Determinism, provenance, and exact unlearning — the three direction-agnostic foundations
identified by external review. Each closes a gap between a documented claim and the code.

### Added
- **Seeded item memory** (`core.SeededHV`, `core.NewSeededTokenDictionary`): token vectors
  derive deterministically from (seed, token) and are bit-identical across runs and machines.
  Committed golden vectors (`core/testdata/`) are verified by CI on ubuntu and macos as the
  standing proof. Seed 0 is bit-compatible with the arena encoder's original stream, so all
  committed arena results remain valid.
- **Provenance ledger + exact unlearning**: `StoreAssociation`/`StoreLabeled` return an
  `AssociationID` and record each association's bound vector and label (~1.3 KB — the price
  of identifiable memories). `RemoveAssociation(id)` exactly unlearns one association via an
  O(D) counter decrement — bit-identical to never having stored it, no retraining (~11 µs,
  zero-alloc). `Ledger`, `FindByLabel`, and `Contributors` expose provenance.
- **First-class glass-box traces**: `PredictionTrace`/`GenerationTrace` carry the symbolic
  derivation, a ranked candidate table (`core.LookupCandidates`; the WebSocket returns up to
  five prompt candidates), explicit stop reasons, and any exact-match ledger
  association(s) for the chosen result. Exposed over the WebSocket API (`"trace": true`) and
  in the terminal UI (`/trace` toggle).

### Changed
- **Breaking:** `NewTokenDictionary()` is now deterministic (seeded with `DefaultSeed`);
  the legacy crypto/rand behavior moved to `NewRandomTokenDictionary()`.
- **Breaking:** memory file format is now v2 (adds vocab seed + ledger). v1 files are
  rejected with a descriptive error; re-train and re-save.
- `StoreAssociation` gained an `AssociationID` return value (existing call sites compile
  unchanged).

## [0.1.0] — 2026-07-23

First public release.

### Added
- Core VSA engine over 10,000-bit hypervectors (`[157]uint64`): bind (XOR), word-level
  permute, majority-vote bundle, `POPCNT` Hamming distance, and a thread-safe item-memory
  dictionary.
- Counter-vector associative memory — O(D) per write, independent of corpus size — with
  `mmap` persistence and a read-only open path that skips the tally and ledger.
- Go AST → hypervector structural encoder.
- Agent trajectory router: a learned (state → next-action) policy with goal- and
  history-dependent tool selection (~1 µs).
- WebSocket API with per-connection agent state; React terminal UI.
- The arena (`arena/`): an honest, reproducible HDC-vs-neural routing benchmark (Go + Python),
  with committed reference results and a crossover report.
- Benchmark harness + `BENCHMARKS.md`; OSS scaffolding (CI, SECURITY, CONTRIBUTING, Code of
  Conduct, issue/PR templates).

### Security
- Confined the `/ast` indexer to a configurable `-index-root`, rejecting absolute paths and
  `..` traversal, with a file-count cap (`parser.MaxIndexFiles`).
- WebSocket origin check defaults to loopback-only; `-allow-all-origins` to opt out.

### Changed
- Honest repositioning: the README leads with measured strengths **and** limitations (citing
  the arena) instead of unbacked "breakthrough" claims.
- Module path is now `github.com/JGautam09/NeuroVSA` (go-get-able).

[0.1.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.1.0
