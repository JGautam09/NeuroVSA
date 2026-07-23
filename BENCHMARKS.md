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
