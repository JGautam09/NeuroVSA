# Reproduce the paired routing benchmark

[Results](RESULTS.md) · [Raw per-query measurements](routing-run.json.gz)

This benchmark compares the existing NeuroVSA HDC router in Go (10,000 bits,
Hamming distance) with Model2Vec `minishlab/potion-base-8M` in Python (normalized
embeddings and cosine similarity). Both classify each query using its nearest
class prototype; the representations, scoring and runtimes differ.

## Run from a checkout containing this harness

Use CPython 3.12.13 and Go 1.26.5 for the reference environment. Run on an idle
machine on CPU. The first run downloads public inputs from their original
sources; no API key is needed.

```sh
trial=$(mktemp -d)
python3.12 -m venv "$trial/venv"
"$trial/venv/bin/python" -m pip install -r arena/routing-refresh/requirements.txt
"$trial/venv/bin/python" arena/datasets/fetch.py
HF_HOME="$trial/hf" "$trial/venv/bin/python" arena/routing_refresh.py \
  --out "$trial/results" --repeats 5
```

The runner checks split hashes and downloads the exact [Potion 8M model
revision](https://huggingface.co/minishlab/potion-base-8M/tree/bf8b056651a2c21b8d2565580b8569da283cab23).
Missing or mismatched inputs fail the run. There is no fallback model or reuse
of previous runs. Reports go to a new directory so reruns cannot overwrite the
checked-in evidence. Once the model is cached, set `HF_HUB_OFFLINE=1` with the
same `HF_HOME` to repeat without downloading it.

To regenerate the checked-in table without installing dependencies or running
either router:

```sh
python3 arena/routing_refresh.py --render-only arena/routing-refresh/routing-run.json.gz
```

## What goes into the measurement

- **Data:** Banking77's official 10,003 training and 3,080 test examples;
  CLINC150's official 15,000 training and 4,500 in-scope test examples. The
  1,000 CLINC150 out-of-scope test examples are not part of this closed-set
  comparison. Dataset sources, checksums and licenses are in
  [the fetcher](../datasets/fetch.py). The separate [curated probe](../dataset.json)
  has 45 canonical and 45 paraphrased queries across 15 intents.
- **Repeats:** Each router trains from the same examples. Within a pair, both
  receive the same 128 untimed queries sampled from training and then the same
  shuffled test order. The seed is recorded in the harness; run order alternates
  between routers. Route predictions must agree across all repeats for a given
  router (not necessarily across routers).
- **Metrics:** Accuracy is correct labels / test queries; macro-F1 averages the
  per-intent F1 scores equally. Per-query wall time includes encoding plus
  nearest-prototype scoring, with model load, training and reporting excluded.
  We take the median of the per-repeat p50 and p99. The p99 min–max spans the
  measured repeats, not a confidence interval or a performance guarantee.
- **Raw evidence:** The gzip contains labels, query indices and nanosecond
  timings, without copying input utterances. It records the Git commit, harness
  hashes, environment, dependencies, input hashes and model file sizes/hashes.
  Use it to recompute scores without downloading the models or datasets.

The run compares two local implementations, not the best possible implementation
of each algorithm. It does not measure throughput, cold start, memory, or
out-of-scope rejection. TF-IDF and MiniLM are available as
[historical reference rows](../ARENA_RESULTS_STANDARD.md), not part of this run.
