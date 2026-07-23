#!/usr/bin/env python3
"""
Neural embedding baseline for the NeuroVSA structured-routing arena.

Same algorithm as the HDC side (arena_test.go): a prototype / nearest-centroid router. The
ONLY difference is the representation — neural semantic embeddings instead of HDC hypervectors
— which is exactly the variable under test. Emits results_neural.json with the identical schema
so report.go can merge the two sides into a crossover table.

Embedding backend (auto-detected, in preference order):
  1. model2vec  (StaticModel, e.g. minishlab/potion-base-8M) — a tiny CPU-only *static*
     semantic embedding: the honest "edge embedding router" competitor to HDC.
  2. sentence-transformers (all-MiniLM-L6-v2) — the classic transformer embedding router.

Install one of:
  python3 -m pip install model2vec
  python3 -m pip install sentence-transformers

Run from the arena/ directory:
  python3 neural_baseline.py
"""

import json
import os
import time
import numpy as np


def load_embedder():
    """Return (name, embed_fn) where embed_fn(list[str]) -> np.ndarray [n, d]."""
    try:
        from model2vec import StaticModel
        model = StaticModel.from_pretrained("minishlab/potion-base-8M")
        return "neural (model2vec potion-base-8M, static CPU)", lambda texts: np.asarray(model.encode(texts), dtype=np.float32)
    except Exception as e:
        print(f"[info] model2vec unavailable ({e}); trying sentence-transformers…")
    from sentence_transformers import SentenceTransformer
    model = SentenceTransformer("all-MiniLM-L6-v2")
    return "neural (all-MiniLM-L6-v2)", lambda texts: np.asarray(model.encode(texts), dtype=np.float32)


def normalize(m):
    n = np.linalg.norm(m, axis=1, keepdims=True)
    n[n == 0] = 1.0
    return m / n


class NeuralRouter:
    def __init__(self, embed):
        self.embed = embed
        self.names = []
        self.protos = None  # [k, d], L2-normalized

    def add_class(self, name, examples):
        vecs = normalize(self.embed(examples))
        proto = normalize(vecs.mean(axis=0, keepdims=True))
        self.protos = proto if self.protos is None else np.vstack([self.protos, proto])
        self.names.append(name)

    def train(self, intents):
        for it in intents:
            self.add_class(it["name"], it["train"])

    def route(self, utterance):
        q = normalize(self.embed([utterance]))          # [1, d]
        sims = (q @ self.protos.T)[0]                    # cosine (normalized)
        idx = int(np.argmax(sims))
        return self.names[idx]


def accuracy_on(router, intents, key):
    correct = total = 0
    for it in intents:
        for u in it[key]:
            total += 1
            if router.route(u) == it["name"]:
                correct += 1
    return correct, total


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    with open(os.path.join(here, "dataset.json")) as f:
        ds = json.load(f)
    intents = ds["intents"]

    name, embed = load_embedder()
    print(f"[info] backend: {name}")

    router = NeuralRouter(embed)
    router.train(intents)

    # Axis 1: accuracy (canonical vs paraphrase)
    cc, ct = accuracy_on(router, intents, "test_canonical")
    pc, pt = accuracy_on(router, intents, "test_paraphrase")

    # Axis 2: latency (embed + route per query)
    probes = [u for it in intents for u in (it["test_canonical"] + it["test_paraphrase"])]
    durs = []
    for _ in range(50):
        for u in probes:
            t0 = time.perf_counter_ns()
            router.route(u)
            durs.append((time.perf_counter_ns() - t0) / 1000.0)  # µs
    durs.sort()

    def pct(p):
        return durs[int(p * (len(durs) - 1))]

    # Axis 3: determinism (within-run route stability). Neural embeddings are floating point,
    # so prototypes are NOT bit-identical across builds/hardware/library versions — the key
    # contrast with HDC's integer-exact, portable representation.
    mismatches = 0
    for u in probes:
        if router.route(u) != router.route(u):
            mismatches += 1

    # Axis 4: cold-add (hold out add_todo, inject at runtime)
    held_name = "add_todo"
    held = next(it for it in intents if it["name"] == held_name)
    train_set = [it for it in intents if it["name"] != held_name]
    r2 = NeuralRouter(embed)
    r2.train(train_set)
    held_tests = held["test_canonical"] + held["test_paraphrase"]
    before = sum(1 for u in held_tests if r2.route(u) == held_name)  # 0 by construction
    t0 = time.perf_counter_ns()
    r2.add_class(held_name, held["train"])
    add_us = (time.perf_counter_ns() - t0) / 1000.0
    after = sum(1 for u in held_tests if r2.route(u) == held_name)

    res = {
        "router": name,
        "dataset": "structured-routing-v1",
        "classes": len(intents),
        "accuracy": {
            "canonical": cc / ct, "paraphrase": pc / pt,
            "canonical_correct": cc, "canonical_total": ct,
            "paraphrase_correct": pc, "paraphrase_total": pt,
        },
        "latency": {"p50_us": pct(0.50), "p99_us": pct(0.99),
                    "mean_us": sum(durs) / len(durs), "samples": len(durs)},
        "determinism": {"runs_compared": 2, "route_mismatches": mismatches,
                        "bit_identical_prototypes": False},
        "cold_add": {"held_out_intent": held_name, "add_latency_us": add_us,
                     "acc_before": before / len(held_tests), "acc_after": after / len(held_tests),
                     "test_phrases": len(held_tests)},
    }

    with open(os.path.join(here, "results_neural.json"), "w") as f:
        json.dump(res, f, indent=2)

    print(f"\n=== Neural Router — Structured Routing Arena ===")
    print(f"Backend: {name} | Classes: {res['classes']}")
    print(f"Accuracy    canonical: {100*cc/ct:.1f}% ({cc}/{ct})   paraphrase: {100*pc/pt:.1f}% ({pc}/{pt})")
    print(f"Latency     p50: {pct(0.50):.2f} µs   p99: {pct(0.99):.2f} µs   mean: {sum(durs)/len(durs):.2f} µs (n={len(durs)})")
    print(f"Determinism within-run route mismatches: {mismatches}   prototypes bit-identical: False (float embeddings)")
    print(f"Cold-add    {held_name!r}: add {add_us:.1f} µs   acc before: {100*before/len(held_tests):.0f}%   after: {100*after/len(held_tests):.0f}% ({len(held_tests)} phrases)\n")


if __name__ == "__main__":
    main()
