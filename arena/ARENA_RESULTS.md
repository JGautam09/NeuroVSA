# Structured-Routing Arena — Results

Same dataset (15 intents), same nearest-centroid routing algorithm. The **only** difference is
the representation: NeuroVSA HDC hypervectors vs. neural embeddings (`neural (model2vec potion-base-8M, static CPU)`). Both measured
on the same machine. Regenerate with the steps in [README.md](README.md).

| Axis | NeuroVSA (HDC) | neural (model2vec potion-base-8M, static CPU) | Winner |
| :--- | :--- | :--- | :--- |
| Canonical accuracy | 100.0% | 100.0% | tie |
| Paraphrase accuracy | 37.8% | 64.4% | Neural |
| Latency p50 (encode+route) | 64.2 µs | 25.0 µs | Neural |
| Latency p99 | 212.5 µs | 49.4 µs | Neural |
| Cold-add latency | 423.4 µs | 206.5 µs | Neural |
| Cold-add accuracy (after) | 83.3% | 83.3% | tie |
| Bit-exact & portable prototypes | yes | no | **HDC** |
| Within-run route determinism | 0 mismatches | 0 mismatches | tie |
| Model artifact required | none (pure algorithm) | yes (downloaded model) | **HDC** |

**Reading it:** both are perfect on canonical/in-grammar phrasing. The neural embedding wins
paraphrase (semantic generalization), and — because this static CPU embedding is just a token
lookup + mean-pool — it also wins latency and cold-add here. HDC's clear wins are integer-exact,
cross-machine-reproducible prototypes and zero model artifact. The honest crossover: HDC is
competitive only where inputs are bounded/canonical and the value is determinism/auditability/
no-dependency deployment — not where free-form paraphrase or raw speed decide it.
