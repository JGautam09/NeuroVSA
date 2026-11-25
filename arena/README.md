# The Arena — routing benchmarks

The arena evaluates **intent routing**: pick a known label from an utterance.
It is separate from the [CI test-selection benchmark](check-selection/RESULTS.md),
which recommends checks for repository changes.

## Current comparison

Start with the [paired NeuroVSA vs. Model2Vec results](routing-refresh/RESULTS.md).
Both implementations ran on CPU in the same session, with the same official
Banking77 and CLINC150 splits and five repeats. A small canonical/paraphrase probe
is reported separately. The [reproduction guide](routing-refresh/README.md) has
pinned inputs, exact commands, and a link to inspectable raw evidence.

The current experiment compares the Go HDC router with a pinned Model2Vec Potion
8M baseline. It measures task accuracy and per-query latency of these
implementations. It does not show that the same speed ordering holds on other
machines or with different scoring implementations.

## Historical results

- [Curated comparison](ARENA_RESULTS.md): 15 controlled intents with canonical and
  paraphrased queries, plus cold-add and determinism probes.
- [Arena v2](ARENA_RESULTS_STANDARD.md): official Banking77 and CLINC150 split
  results with TF-IDF, Model2Vec and MiniLM, including older cold-add and
  out-of-scope measurements. Those rows are from the historical run and are not
  part of the current paired table.
- [Dimensionality study](../docs/DIMENSIONALITY.md): accuracy, capacity and cost
  at multiple binary hypervector sizes.

The legacy test and Python entry points remain available. Do not regenerate or
combine the historical committed tables when reproducing the paired benchmark.
