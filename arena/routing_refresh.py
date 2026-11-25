#!/usr/bin/env python3
"""Paired CPU benchmark; use --render-only to regenerate from saved raw evidence."""

import argparse
from collections import Counter
from datetime import datetime, timezone
import gzip
import hashlib
import importlib.metadata
import json
import math
import os
from pathlib import Path
import platform
import random
import statistics
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent
MODEL = "minishlab/potion-base-8M"
REVISION = "bf8b056651a2c21b8d2565580b8569da283cab23"
MODEL_FILES = ("config.json", "model.safetensors", "tokenizer.json")
ROUTERS = ("NeuroVSA HDC", "Model2Vec Potion 8M")
EXPECTED_COUNTS = {"banking77": 3080, "clinc150": 4500,
                   "curated-canonical": 45, "curated-paraphrase": 45}
DATASET_HASHES = {
    "banking77.arena.json": "a9dea32f0c33db27720d7a93498d759daa3840dabb43c249768e0d2165d08e40",
    "clinc150.arena.json": "327356989cd14be9445060eac036ddf46947307686a9544e6feb4d7c77b81228",
    "dataset.json": "32623aadaae34302f1e6010408ce5081ed9d56510c138cf6bb2f2b7db0561ebb",
}
THREADS = dict(GOMAXPROCS="1", OMP_NUM_THREADS="1", OPENBLAS_NUM_THREADS="1",
               VECLIB_MAXIMUM_THREADS="1", MKL_NUM_THREADS="1", BLIS_NUM_THREADS="1",
               RAYON_NUM_THREADS="1", TOKENIZERS_PARALLELISM="false")
WARMUP = 128


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def command(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def cohorts():
    specs = [
        ("banking77", HERE / "datasets/banking77.arena.json", "test"),
        ("clinc150", HERE / "datasets/clinc150.arena.json", "test"),
        ("curated-canonical", HERE / "dataset.json", "test_canonical"),
        ("curated-paraphrase", HERE / "dataset.json", "test_paraphrase"),
    ]
    for name, source, split in specs:
        digest = sha(source)
        if digest != DATASET_HASHES[source.name]:
            raise ValueError(f"{source.name} does not match the pinned split")
        intents = sorted(json.loads(source.read_text())["intents"], key=lambda i: i["name"])
        classes = [{"name": i["name"], "train": i["train"]} for i in intents]
        cases = [(text, i["name"]) for i in intents for text in i[split]]
        if len(cases) != EXPECTED_COUNTS[name] or any(not c["train"] for c in classes):
            raise ValueError(f"unexpected {name} split")
        yield name, str(source.relative_to(ROOT)), digest, classes, cases


def load_model():
    from huggingface_hub import hf_hub_download
    from model2vec import StaticModel

    files = [Path(hf_hub_download(MODEL, f, revision=REVISION, token=False)) for f in MODEL_FILES]
    if any(f.parent.name != REVISION for f in files):
        raise ValueError("model files were not resolved at the pinned revision")
    return StaticModel.from_pretrained(files[0].parent), {
        f.name: {"sha256": sha(f), "bytes": f.stat().st_size} for f in files
    }


def run_model(model, classes, warmup, texts):
    from standard_baseline import DenseRep
    import numpy as np

    def encode(samples):
        v = np.asarray(model.encode(samples, use_multiprocessing=False), dtype=np.float32)
        n = np.linalg.norm(v, axis=1, keepdims=True)
        n[n == 0] = 1
        return list(v / n)

    rep = DenseRep("model2vec", encode)
    prototypes = [(c["name"], rep.centroid(rep.encode_many(c["train"]))) for c in classes]

    def route(text):
        probe = rep.encode(text)
        best, score = None, -math.inf
        for name, prototype in prototypes:
            sim = rep.sim(probe, prototype)
            if sim > score:
                best, score = name, sim
        return best

    for text in warmup:
        route(text)
    samples = []
    for text in texts:
        start = time.perf_counter_ns()
        outcome = route(text)
        elapsed = time.perf_counter_ns() - start
        samples.append({"prediction": outcome, "ns": elapsed})
    return samples


def run_hdc(binary, work, classes, warmup, texts):
    source, result = work / "input.json", work / "hdc.json"
    source.write_text(json.dumps(dict(classes=classes, warmup=warmup, test=texts)))
    run = subprocess.run([str(binary), "-test.run", "^TestArenaRoutingRefresh$"],
                         env=dict(os.environ, ARENA_REFRESH_INPUT=str(source),
                                  ARENA_REFRESH_OUTPUT=str(result)), capture_output=True, text=True)
    if run.returncode:
        raise RuntimeError(f"HDC worker failed:\n{run.stdout}{run.stderr}")
    return json.loads(result.read_text())


def cpu_label():
    if platform.system() == "Darwin":
        data = json.loads(command("system_profiler", "SPHardwareDataType", "-json"))
        return data["SPHardwareDataType"][0].get("chip_type", platform.machine())
    return platform.processor() or platform.machine()


def benchmark(repeats):
    os.environ.update(THREADS)
    os.environ.update(HF_HUB_DISABLE_TELEMETRY="1", HF_HUB_DISABLE_IMPLICIT_TOKEN="1")
    # Pin the implementation checkout. The isolated runner may add harness/outputs,
    # but may not modify the existing Go router or replay inputs.
    subprocess.run(["git", "diff", "--quiet", "HEAD", "--", "core", "go.mod", "go.sum",
                    "arena/classifier.go", "arena/encoder.go", "arena/dataset.json",
                    "arena/standard_baseline.py", "arena/datasets/fetch.py"], cwd=ROOT, check=True)
    model, model_hashes = load_model()
    suites = list(cohorts())
    with tempfile.TemporaryDirectory(prefix="nvsa-routing-") as directory:
        work = Path(directory)
        binary = work / "arena.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./arena"],
                       cwd=ROOT, check=True)
        raw = dict(schema=1, started_utc=datetime.now(timezone.utc).isoformat(),
                   commit=command("git", "rev-parse", "HEAD"), cpu=cpu_label(),
                   os=f"{platform.system()} {platform.mac_ver()[0] or platform.release()}",
                   arch=platform.machine(), go=command("go", "version"),
                   python=platform.python_version(),
                   packages=sorted(f"{d.metadata['Name']}=={d.version}"
                                   for d in importlib.metadata.distributions()),
                   model=dict(name=MODEL, revision=REVISION, files=model_hashes),
                   harness_sha256={p.name: sha(p) for p in
                                   (Path(__file__), HERE / "routing_refresh_test.go")},
                   warmup=WARMUP, thread_environment=THREADS, repeats=repeats, cohorts={})
        for name, relative, digest, classes, cases in suites:
            raw["cohorts"][name] = dict(source=relative, sha256=digest, gold=[g for _, g in cases],
                                        classes=[c["name"] for c in classes], rounds=[])
        for repeat in range(repeats):
            # Alternate which router goes first within a pair on every repeat.
            order = ROUTERS if repeat % 2 == 0 else ROUTERS[::-1]
            for name, _, _, classes, cases in suites:
                rng = random.Random(2601 + repeat)
                train = [text for c in classes for text in c["train"]]
                warmup = rng.choices(train, k=WARMUP)
                indices = list(range(len(cases)))
                rng.shuffle(indices)
                texts = [cases[i][0] for i in indices]
                result = {}
                for router in order:
                    if router == ROUTERS[0]:
                        result[router] = run_hdc(binary, work, classes, warmup, texts)
                    else:
                        result[router] = run_model(model, classes, warmup, texts)
                raw["cohorts"][name]["rounds"].append(
                    dict(order=order, query_indices=indices, samples=result))
            print(f"Paired round {repeat + 1}/{repeats} complete", flush=True)
    return raw


def summarize(raw):
    if raw["schema"] != 1 or set(raw["cohorts"]) != set(EXPECTED_COUNTS) or raw["repeats"] < 2:
        raise ValueError("unsupported or incomplete benchmark")
    if (raw["model"]["name"], raw["model"]["revision"]) != (MODEL, REVISION):
        raise ValueError("wrong Model2Vec baseline")
    summary = {}
    for name, cohort in raw["cohorts"].items():
        gold, rounds = cohort["gold"], cohort["rounds"]
        count = len(gold)
        classes = set(cohort["classes"])
        pinned = DATASET_HASHES[Path(cohort["source"]).name]
        if (cohort["sha256"] != pinned or count != EXPECTED_COUNTS[name]
                or len(rounds) != raw["repeats"] or set(gold) != classes):
            raise ValueError(f"bad cohort {name}")
        for j, round_ in enumerate(rounds):
            if (sorted(round_["query_indices"]) != list(range(count))
                    or set(round_["samples"]) != set(ROUTERS)
                    or tuple(round_["order"]) != (ROUTERS if j % 2 == 0 else ROUTERS[::-1])):
                raise ValueError(f"bad pair in {name}")
        for router in ROUTERS:
            p50, p99, reference = [], [], None
            for round_ in rounds:
                samples = round_["samples"][router]
                if len(samples) != count or any(s["ns"] <= 0 or s["prediction"] not in classes for s in samples):
                    raise ValueError(f"incomplete samples for {name}/{router}")
                reordered = [None] * count
                for i, sample in zip(round_["query_indices"], samples):
                    reordered[i] = sample["prediction"]
                if reference is not None and reference != reordered:
                    raise ValueError(f"inconsistent labels across repeats: {name}/{router}")
                reference = reordered
                ns = sorted(s["ns"] for s in samples)
                p50.append(ns[math.ceil(0.50 * count) - 1] / 1000)
                p99.append(ns[math.ceil(0.99 * count) - 1] / 1000)
            outcomes = Counter(zip(gold, reference))
            correct = sum(outcomes[c, c] for c in classes)
            observed, predicted = Counter(gold), Counter(reference)
            f1 = statistics.mean(2 * outcomes[c, c] / (observed[c] + predicted[c]) for c in classes)
            summary[name, router] = dict(n=count, correct=correct, f1=f1, p50=statistics.median(p50),
                                         p99=statistics.median(p99), p99_min=min(p99), p99_max=max(p99))
    return summary


def render(raw):
    s = summarize(raw)
    rows = []
    for name, label in (("banking77", "Banking77"), ("clinc150", "CLINC150")):
        for router in ROUTERS:
            m = s[name, router]
            rows.append(f"| {label} | {router} | {m['correct']:,}/{m['n']:,} "
                        f"({m['correct']/m['n']:.1%}) | {m['f1']:.3f} | {m['p50']:.1f} | "
                        f"{m['p99']:.1f} ({m['p99_min']:.1f}–{m['p99_max']:.1f}) |")
    canon = [s["curated-canonical", r]["correct"] for r in ROUTERS]
    para = [s["curated-paraphrase", r]["correct"] for r in ROUTERS]
    return f"""# Paired routing benchmark: NeuroVSA vs. Model2Vec

Run on {raw['started_utc'][:10]} (UTC), {raw['cpu']} ({raw['os']}, {raw['arch']}),
with {raw['repeats']} paired repeats. NeuroVSA source commit: `{raw['commit']}`.
Model2Vec: [{MODEL}](https://huggingface.co/{MODEL}/tree/{REVISION}).

| Official test split | Router | Accuracy | Macro-F1 | p50 (µs) | p99 (µs; repeat min–max) |
| --- | --- | ---: | ---: | ---: | ---: |
{chr(10).join(rows)}

**Curated phrasing probe (directional):** canonical accuracy was {canon[0]}/45 for
NeuroVSA and {canon[1]}/45 for Model2Vec; paraphrase accuracy was {para[0]}/45 and
{para[1]}/45, respectively. The small probe is separate from the official splits.

Latency includes encoding and one nearest-prototype lookup per query. Reported
p50 and p99 are medians of the corresponding per-repeat nearest-rank percentiles;
p99 ranges show run-to-run spread, not confidence intervals. Both routers used the
same queries in the same shuffled order within each pair, {raw['warmup']} untimed
training-example warmups, and one worker. Downloads, model loading, training and
reporting are excluded. CLINC150 uses only the 4,500 in-scope test queries here.

This measures the shipped routing implementations (Go/Hamming versus
Python/NumPy/cosine), not a language-independent speed property of either
representation. No cold-start, memory-footprint or out-of-scope claim is made by
this rerun. See [the reproduction guide](README.md) and
[raw per-query samples](routing-run.json.gz) for provenance and exact inputs.
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, help="new output directory")
    parser.add_argument("--repeats", type=int, default=5)
    parser.add_argument("--render-only", type=Path, help="read a saved raw gzip and print Markdown")
    args = parser.parse_args()
    if args.render_only:
        with gzip.open(args.render_only, "rt", encoding="utf-8") as f:
            print(render(json.load(f)), end="")
        return
    if args.out is None or args.out.exists() or not 2 <= args.repeats <= 20:
        parser.error("provide a new --out directory and 2–20 repeats")
    raw = benchmark(args.repeats)
    report = render(raw)
    args.out.mkdir(parents=True)
    (args.out / "routing-run.json.gz").write_bytes(
        gzip.compress(json.dumps(raw, separators=(",", ":")).encode(), mtime=0))
    (args.out / "RESULTS.md").write_text(report, encoding="utf-8")


if __name__ == "__main__":
    main()
