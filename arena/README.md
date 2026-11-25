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

## Arena v2 — standard datasets (CLINC150, Banking77)

The "re-run on standard datasets" caveat is now retired: the official train/test splits of
**CLINC150** (150 intents + 1000 out-of-scope test queries) and **Banking77** (77 intents),
fetched and SHA-256-pinned by [`datasets/fetch.py`](datasets/fetch.py), run through the same
nearest-centroid protocol for four representations — HDC, TF-IDF (stdlib floor), model2vec,
and **all-MiniLM-L6-v2** (the full transformer the roadmap named). Generated tables:
[`ARENA_RESULTS_STANDARD.md`](ARENA_RESULTS_STANDARD.md). The measured story, one Apple M5 Pro:

- **Accuracy (the language axis): HDC comes last on both datasets** — 69.1% / 71.8%
  (Banking77 / CLINC150) vs TF-IDF 80.7% / 83.3%, model2vec 79.6% / 85.9%, MiniLM
  84.9% / 91.7%. Even a stdlib TF-IDF beats the HDC n-gram encoder on real corpora —
  stated plainly.
- **Latency: HDC is fastest** — ~32 µs p50 vs MiniLM's ~2.9 ms (≈90×). The old caveat
  *predicted* a full transformer would cost ~1–10 ms and lose the latency axis while
  winning accuracy; the measurement confirms it on both counts.
- **Out-of-scope detection (CLINC150) tracks accuracy**: min-distance AUROC 0.804 (HDC),
  0.871 (TF-IDF), 0.926 (model2vec), 0.966 (MiniLM).

```bash
python3 arena/datasets/fetch.py                                  # pinned download + convert
ARENA_STANDARD=1 go test ./arena -run TestArenaHDCStandard -v    # HDC side
python3 -m pip install model2vec sentence-transformers           # baselines (venv suggested)
python3 arena/standard_baseline.py                               # tfidf + model2vec + minilm
ARENA_STANDARD=1 go test ./arena -run TestArenaStandardReport -v # merge tables
```

## Honest caveats

- **The curated corpus remains directional.** 15 intents, 45 phrases per split — useful as
  the canonical-vs-paraphrase probe, superseded by the standard datasets above for hard
  conclusions.
- **Speed comparisons are same-machine indicative**, not universal: every number in the
  tables was measured on the one reference machine, and result files record which run
  produced them. A missing row means a missing measurement, never a hidden loss.
- **Determinism nuance.** Both routers are stable *within* a run (0 mismatches). The real HDC
  advantage is that its integer prototypes are bit-identical *across* hardware, builds, and
  library versions; float embeddings are not.

## Bottom line

HDC is competitive only where inputs are **bounded/canonical** and the value is
**determinism, auditability, or zero-dependency deployment** — on real intent corpora it
loses the accuracy axis to every baseline tried, including stdlib TF-IDF, while winning
raw per-query latency. For open-vocabulary conversational/voice routing, a small semantic
embedding router is the stronger default.
