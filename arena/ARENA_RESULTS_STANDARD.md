# Arena v2 — Standard Datasets

Official train/test splits of CLINC150 (150 intents, + 1000 out-of-scope test queries)
and Banking77 (77 intents), fetched and converted by [datasets/fetch.py](datasets/fetch.py)
(SHA-256-pinned; licenses noted there). Same nearest-centroid protocol for every router —
only the representation differs. Regenerate: fetch, then ARENA_STANDARD=1 with
TestArenaHDCStandard (Go side), standard_baseline.py (Python side), and
TestArenaStandardReport.

## banking77

| Router | Accuracy | Macro-F1 | p50 µs | p99 µs | Cold-add µs | Cold-add acc | OOS AUROC |
| :-- | --: | --: | --: | --: | --: | --: | --: |
| hdc | 69.1% | 0.694 | 31.3 | 143.9 | 7944 | 70.0% | — |
| minilm | 84.9% | 0.848 | 2865.0 | 5034.7 | 120472 | 90.0% | — |
| model2vec | 79.6% | 0.796 | 52.5 | 86.0 | 1986 | 85.0% | — |
| tfidf | 80.7% | 0.807 | 45.9 | 138.8 | 1054 | 82.5% | — |

## clinc150

| Router | Accuracy | Macro-F1 | p50 µs | p99 µs | Cold-add µs | Cold-add acc | OOS AUROC |
| :-- | --: | --: | --: | --: | --: | --: | --: |
| hdc | 71.8% | 0.723 | 32.3 | 72.9 | 1686 | 63.3% | 0.804 |
| minilm | 91.7% | 0.915 | 2844.1 | 3622.7 | 38902 | 90.0% | 0.966 |
| model2vec | 85.9% | 0.857 | 75.8 | 101.4 | 1007 | 86.7% | 0.926 |
| tfidf | 83.3% | 0.831 | 78.3 | 161.5 | 362 | 86.7% | 0.871 |

Read it straight: routers appear only if their run completed on this
machine — a missing row is a missing measurement, never a hidden loss. Cross-router speed
comparisons are same-machine indicative; accuracy and macro-F1 are split-exact.
