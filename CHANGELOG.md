# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] — 0.3.0 "NeuroMesh + ProofRoute"

Mergeable memories (the provenance ledger becomes a true CRDT) and verifiable decisions.

### Added
- **Decision certificates (ProofRoute)**: `IssueDecision` produces a machine-checkable
  receipt — state vector, candidate vocabulary, ranked table, contributors, and the memory's
  `Fingerprint` — that any holder of the memory can RE-EXECUTE to bit-exact agreement
  (`VerifyAgainst`). Optional ed25519 signatures over a deterministic binary encoding (never
  JSON). Traced `/route` responses now carry a certificate; `SelectNextToolCertified` issues
  them for agent trackers.
- **Signed lesson packs**: a pack is a mini-replica (fixed site + sequences), so
  `ApplyPack` = a NeuroMesh merge — idempotent, deduplicating across replicas that applied
  the same pack — and `RevokePack` tombstones the unit, with revocation propagating through
  merges. `PackFromMemory` is the authoring flow; ed25519 signing; hex-vector JSON.
- **`cmd/nvsa-verify`**: CLI that verifies certificates against a memory file (signature +
  fingerprint + bit-exact re-execution) and packs (signature + installation status).
- **RuleGarden integration (the arc closes)**: creature brains claim deterministic per-world
  sites, enabling **merge brains across worlds** — logged as a replayable event that embeds
  the foreign pack (merged worlds still replay bit-exactly; failed merges are atomic no-ops).
  The creature inspector issues **downloadable decision receipts** (+ brain image) that
  `nvsa-verify` re-executes natively — demonstrated end-to-end from a browser-issued receipt.
  `MarshalBinary`/`UnmarshalBinary` expose the v3 memory image for wasm and atomic clones.
- **`Merge(other)`** — associative memories now merge like replicas: ledger union with
  monotone tombstones (forgetting propagates), content-collision detection, and a
  `MergeReport` (added/shared/tombstones/capacity warning against the measured G0 envelope).
  Commutativity, associativity, idempotence, and convergence under gossip interleavings are
  proven by property tests with bit-exact fingerprint equality.
- **`Fingerprint()`** — canonical SHA-256 over the convergent state (ledger set + tombstones
  + vocab seed, writer identity excluded); also the memory-state anchor for upcoming decision
  certificates.
- **RuleGarden**: deterministic teachable world (headless Go package, wasm bridge, browser
  page) — teach one-shot lessons, transfer by analogy, forget exactly; glass-box decisions
  with lesson/generalization/instinct bases; worlds export as seed+event-log packs with
  golden-tested replay determinism. G0 capacity gate published in BENCHMARKS.md.

### Changed
- **Breaking:** `AssociationID` is now composite `(Site, Seq)` — `"site:seq"` in JSON — so
  distinct writers can never collide; `SetSite` claims writer identity (and recomputes the
  next sequence for the chosen site, so switching to a merged-in site cannot mint a colliding
  ID).
- **Breaking:** memory file format is v3 (adds site; composite-ID ledger entries; canonical
  entry order). v1/v2 files are rejected with a descriptive error.

### Security / hardening (post-review)
- The memory loader now bounds the untrusted ledger count against the file's byte budget
  before allocating, parses into local state, and **rebuilds the tally and majority vector
  from the ledger** (never trusting the serialized copies the fingerprint doesn't cover) —
  so a malformed or matrix-tampered image cannot crash `nvsa-verify` or slip past its anchor.
- Decision certificates gained re-derivable `ExecutedAction`/`Basis` fields: RuleGarden
  receipts now certify the action actually taken (instinct overrides included), issued at
  decision time against the decision-time memory image, so teaching/forgetting afterward
  cannot make a receipt certify a different historical decision.
- World replay is bounded (pack byte size, tick horizon, event count, merge-nesting depth),
  so a pasted pack cannot freeze the browser.
- `Pack.Memory` rejects labels over the 64 KiB serialization limit.

## [0.2.0] — "Foundations"

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
