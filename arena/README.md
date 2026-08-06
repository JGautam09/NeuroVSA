# The Arena — HDC vs. Neural Router (structured routing)

A head-to-head, apples-to-apples benchmark. **Both** routers are the *same* prototype /
nearest-centroid classifier; the **only** variable is the representation — NeuroVSA HDC
hypervectors vs. neural embeddings. It measures where the deterministic HDC router genuinely
wins and where it doesn't, so a product bet rests on data instead of hope.

## Method

- **Dataset** (`dataset.json`): 15 agent/voice tool intents. Each has training phrases, a
  held-out **canonical** test split (same phrasing style), and a **paraphrase** test split
  (same meaning, deliberately different vocabulary). The canonical-vs-paraphrase split is the
  crossover probe: surface-form matching (HDC) should hold on canonical and fall on paraphrase;
  semantic embeddings should hold on both.
- **HDC side** (`*.go`): a deterministic, *seeded* n-gram encoder (token → hypervector via a
  hash-seeded splitmix64, **not** crypto-random) so prototypes are bit-identical across runs and
  machines. Class prototype = `Bundle` of training-utterance vectors; route = min-Hamming class.
- **Neural side** (`neural_baseline.py`): identical nearest-centroid logic over embeddings.
  Backend auto-detects `model2vec` (a tiny static CPU embedding — the honest "edge embedding
  router") or falls back to `sentence-transformers` (all-MiniLM-L6-v2).
- **Four axes:** accuracy (canonical vs paraphrase), latency p50/p99 (encode+route per query),
  determinism (bit-exact + within-run stability), cold-add (inject a brand-new intent at runtime).

## Reproduce

```bash
# HDC side (writes results_hdc.json)
go test ./arena/ -run TestArenaHDC -v

# Neural side (writes results_neural.json)
python3 -m pip install model2vec        # or: sentence-transformers
python3 arena/neural_baseline.py

# Merge into ARENA_RESULTS.md
go test ./arena/ -run TestArenaReport -v

# Precise per-query HDC latency
go test ./arena/ -bench BenchmarkRoute -benchmem
```

## Headline result

See [ARENA_RESULTS.md](ARENA_RESULTS.md) for the generated table. On this dataset, measured on
one Apple M5 Pro, the tiny static embedding (`model2vec potion-base-8M`) **beat** the HDC router
decisively on paraphrase accuracy (64% vs 38%) — the axis that matters for language. After the
v0.9.0 bit-sliced `Bundle` (13× on the encode's dominant op, measured back-to-back), the HDC side
re-measures at 15.9 µs p50 / 94 µs cold-add vs the neural side's committed 25 µs / 206 µs — the
latency and cold-add axes flip to HDC, with a caveat stated honestly: the neural JSON is the
prior committed run on the same machine, not a same-session pair; both sides get re-measured
together in the arena-v2 rerun. HDC's structural wins are unchanged: **bit-exact,
cross-machine-reproducible prototypes** and **zero model artifact** (pure algorithm, ~KB, no
downloaded model). Canonical accuracy tied at 100%.

## Honest caveats

- **Small, curated dataset.** 15 intents, 45 test phrases per split — directional, not
  publication-grade. Re-run on CLINC150 / Banking77 before drawing hard conclusions.
- **The baseline is a *static* embedding.** model2vec is a token-lookup + mean-pool, which is
  why it is so fast. A full transformer (all-MiniLM via sentence-transformers) would be
  ~1–10 ms/query, and HDC would win the latency axis — but **not** the paraphrase-accuracy axis,
  which is the one that decides free-form natural-language routing.
- **HDC latency is `Bundle`-dominated and further optimizable** (a bit-sliced majority could cut
  it several-fold). That could flip the latency axis vs. model2vec; it does **not** close the
  semantic paraphrase gap.
- **Determinism nuance.** Both routers are stable *within* a run (0 mismatches). The real HDC
  advantage is that its integer prototypes are bit-identical *across* hardware, builds, and
  library versions; float embeddings are not.

## Bottom line

HDC is competitive only where inputs are **bounded/canonical** and the value is
**determinism, auditability, or zero-dependency deployment** — not where free-form paraphrase
understanding or raw speed decide the outcome. For open-vocabulary conversational/voice routing,
a small semantic embedding router is the stronger default.
