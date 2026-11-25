# Paired routing benchmark: NeuroVSA vs. Model2Vec

Run on 2026-11-25 (UTC), Apple M5 Pro (Darwin 27.2.1, arm64),
with 5 paired repeats. NeuroVSA source commit: `4250ad62902d8287aaf739e0fa965e9c755bb94f`.
Model2Vec: [minishlab/potion-base-8M](https://huggingface.co/minishlab/potion-base-8M/tree/bf8b056651a2c21b8d2565580b8569da283cab23).

| Official test split | Router | Accuracy | Macro-F1 | p50 (µs) | p99 (µs; repeat min–max) |
| --- | --- | ---: | ---: | ---: | ---: |
| Banking77 | NeuroVSA HDC | 2,127/3,080 (69.1%) | 0.694 | 44.5 | 226.4 (204.1–254.9) |
| Banking77 | Model2Vec Potion 8M | 2,453/3,080 (79.6%) | 0.796 | 80.5 | 233.7 (223.3–244.7) |
| CLINC150 | NeuroVSA HDC | 3,229/4,500 (71.8%) | 0.723 | 47.5 | 132.7 (130.4–149.9) |
| CLINC150 | Model2Vec Potion 8M | 3,864/4,500 (85.9%) | 0.857 | 117.4 | 278.9 (264.5–295.2) |

**Curated phrasing probe (directional):** canonical accuracy was 45/45 for
NeuroVSA and 45/45 for Model2Vec; paraphrase accuracy was 17/45 and
29/45, respectively. The small probe is separate from the official splits.

Latency includes encoding and one nearest-prototype lookup per query. Reported
p50 and p99 are medians of the corresponding per-repeat nearest-rank percentiles;
p99 ranges show run-to-run spread, not confidence intervals. Both routers used the
same queries in the same shuffled order within each pair, 128 untimed
training-example warmups, and one worker. Downloads, model loading, training and
reporting are excluded. CLINC150 uses only the 4,500 in-scope test queries here.

This measures the shipped routing implementations (Go/Hamming versus
Python/NumPy/cosine), not a language-independent speed property of either
representation. No cold-start, memory-footprint or out-of-scope claim is made by
this rerun. See [the reproduction guide](README.md) and
[raw per-query samples](routing-run.json.gz) for provenance and exact inputs.
