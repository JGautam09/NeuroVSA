# Benchmarks

Reproduce all figures with:

```bash
go test -bench . -benchmem -run '^$' ./...
```

The benchmark harness is committed alongside the code:

- `core/bench_test.go` — VSA primitives (`Bind`, `Permute`, `Bundle`, `HammingDistance`)
- `engine/bench_test.go` — `StoreAssociation` (associative-memory write) and `SelectNextTool` (agent tool routing)

## Reference run

- **Machine:** Apple M5 Pro (`darwin/arm64`)
- **Toolchain:** Go 1.26
- All operations are zero-allocation (`0 B/op`, `0 allocs/op`).

| Benchmark | Time / op | Notes |
| :--- | :--- | :--- |
| `Bind` (⊗) | ~76 ns | XOR across 157 `uint64` words |
| `Permute` (ρ) | ~286 ns | Word-level cyclic rotate over the 10,000-bit ring |
| `Bundle8` (⊕) | ~37 µs | Majority vote over 8 vectors (word-level counter tally) |
| `HammingDistance` | ~43 ns | `POPCNT` via `math/bits.OnesCount64` |
| `StoreAssociation` | ~11.7 µs | O(D) per write, independent of corpus size N (incl. ~1.3 KB ledger provenance append) |
| `RemoveAssociation` | ~10.9 µs | Exact unlearning: O(D) counter decrement + rematerialize, zero-alloc |
| `SelectNextTool` | ~1.03 µs | Policy unbind + Hamming cleanup over the tool set |

## Memory capacity (G0 gate)

How many (context → action) associations fit in ONE associative memory before recall
degrades? Measured by `engine/capacity_test.go` with cleanup over a 4-action vocabulary
(the RuleGarden creature-brain regime). Reproduce with:

```bash
go test ./engine/ -run CapacityCurve -v
```

| K (associations) | Random contexts | Structured percepts (shared fillers) |
| ---: | :--- | :--- |
| 4 | 100% (margin ~1862 bits) | 100% (margin ~938) |
| 32 | 100% (~653) | 100% (~841) |
| 64 | 100% (~449) | 100% (~853) |
| 96 | — | **100% (~846) — full RuleGarden percept space** |
| 128 | 100% (~314) | — |
| 256 | 100% (~211) | — |
| 512 | 98.6% (~134) | — |

**Interpretation:** margins shrink ~1/√K exactly as VSA theory predicts; recall stays perfect
through K=256 for orthogonal contexts and across the entire structured percept space. The
engine exposes `RecommendedMaxActiveAssociations = 128` as the documented safe ceiling —
beyond it, degradation is graceful (toward the noise floor), not loud. Single memories are
**not** unbounded stores; that is a design envelope, stated plainly.

## Notes

- `Permute` is implemented as an O(NumWords) word-level rotate (two linear multiword shifts +
  OR + mask), not an O(Dimension) bit loop — the earlier bit-by-bit version measured ~9.85 µs
  for the same operation.
- `StoreAssociation` updates a fixed `[Dimension]uint32` counter vector in place, so its cost
  does not grow with the number of previously stored associations. Since v0.2.0 each write
  also appends a ~1.3 KB provenance ledger entry — the price of identifiable memories, which
  is what makes `RemoveAssociation` exact and glass-box traces able to name contributors.
- `RemoveAssociation` is the counter-decrement inverse of a store: the result is bit-identical
  to a memory that never stored the entry, with no retraining and no dependence on corpus size.
- `Bundle` tallies set bits word-by-word (via `TrailingZeros64`) into a counter, then
  thresholds once — output is bit-identical to the naive definition. It still scales with the
  number of vectors bundled; a bit-sliced majority is the next optimization (see the roadmap).

## G2 — AST encoder v2: structural retrieval (renamed-twin corpus)

The question the gate asks: *can the encoder find a function whose **shape** matches when
every identifier has been renamed?* — the case a code-search encoder exists for. The corpus
(`parser/ast_v2_test.go`, `TestStructuralRetrievalGate`) holds 6 query functions, their 6
**structural twins** (same signature shape and statement stream, every name changed), and 6
**name-bait distractors** (same parameter names / near-identical function names as the
queries, different structure). Precision@1 = the query's top-ranked neighbor is its twin.

| Encoder | What it sees | P@1 (renamed twins) | Failure mode |
| :--- | :--- | :--- | :--- |
| v1 (names only) | func/param/return names | **0 / 6** | picks the name-bait every single time |
| v2 (structural) | + receiver/param/return **types**, statement **kinds**, position-permuted **control-flow stream** | **6 / 6** | — |

Both encoders are deterministic under the seeded dictionary; v2's cross-platform
determinism is pinned by a SHA-256 golden (`TestEncoderV2DeterminismGolden`, enforced by CI
on ubuntu and macos), and v1's exact historical output is protected by
`TestEncoderV1Unchanged`.

Honest limits: this measures **structural** similarity on a small curated corpus — v2 does
not understand meaning (a renamed twin with a *reordered but equivalent* body scores lower;
semantically different code with an identical statement-kind stream scores high). Names
still contribute signal in v2, so name matches help when they exist. The server flag
`-ast-encoder 1` keeps the legacy behavior.
