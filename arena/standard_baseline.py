#!/usr/bin/env python3
"""Neural/lexical baselines for the standard-dataset arena (CLINC150, Banking77).

Same nearest-centroid protocol as the Go HDC side — only the representation differs:

  tfidf     TF-IDF bag-of-words, cosine nearest centroid. Pure stdlib, always runs —
            the floor any representation must beat.
  model2vec Static CPU embedding (potion-base-8M), the curated arena's baseline.
  minilm    all-MiniLM-L6-v2 via sentence-transformers — the "full transformer" the
            original roadmap named.

Each optional model is attempted and skipped with an honest note if its import fails;
a missing results file means a missing measurement, never a hidden loss.

Usage: python3 arena/standard_baseline.py [tfidf|model2vec|minilm ...]
"""

import json
import math
import pathlib
import statistics
import sys
import time
from collections import Counter, defaultdict

HERE = pathlib.Path(__file__).resolve().parent
DATASETS = sorted(HERE.glob("datasets/*.arena.json"))


# ---- metrics (mirrors arena/standard.go) ----

def macro_f1(gold, pred, classes):
    tp, fp, fn, support = Counter(), Counter(), Counter(), Counter()
    for g, p in zip(gold, pred):
        support[g] += 1
        if g == p:
            tp[g] += 1
        else:
            fp[p] += 1
            fn[g] += 1
    scores = []
    for c in classes:
        if support[c] == 0:
            continue
        dp, dr = tp[c] + fp[c], tp[c] + fn[c]
        if dp == 0 or dr == 0 or tp[c] == 0:
            scores.append(0.0)
            continue
        p, r = tp[c] / dp, tp[c] / dr
        scores.append(2 * p * r / (p + r))
    return sum(scores) / len(scores) if scores else 0.0


def auroc(pos, neg):
    """Rank-based AUROC; positives should score HIGHER."""
    allpts = sorted([(s, True) for s in pos] + [(s, False) for s in neg])
    rank_sum, i = 0.0, 0
    while i < len(allpts):
        j = i
        while j < len(allpts) and allpts[j][0] == allpts[i][0]:
            j += 1
        avg = (i + 1 + j) / 2
        rank_sum += avg * sum(1 for k in range(i, j) if allpts[k][1])
        i = j
    n_pos, n_neg = len(pos), len(neg)
    return (rank_sum - n_pos * (n_pos + 1) / 2) / (n_pos * n_neg)


def percentile(sorted_xs, p):
    return sorted_xs[int(p * (len(sorted_xs) - 1))] if sorted_xs else 0.0


# ---- representations ----

class TfidfRep:
    """Sparse TF-IDF vectors with cosine similarity. Stdlib only."""

    name = "tfidf"

    def fit(self, train_texts):
        df = Counter()
        for t in train_texts:
            df.update(set(self._toks(t)))
        self.n_docs = len(train_texts)
        self.idf = {w: math.log(self.n_docs / c) for w, c in df.items()}

    @staticmethod
    def _toks(text):
        return [w for w in "".join(ch if ch.isalnum() else " " for ch in text.lower()).split()]

    def encode(self, text):
        tf = Counter(self._toks(text))
        vec = {w: c * self.idf.get(w, 0.0) for w, c in tf.items()}
        norm = math.sqrt(sum(v * v for v in vec.values())) or 1.0
        return {w: v / norm for w, v in vec.items()}

    @staticmethod
    def centroid(vecs):
        acc = defaultdict(float)
        for v in vecs:
            for w, x in v.items():
                acc[w] += x
        norm = math.sqrt(sum(v * v for v in acc.values())) or 1.0
        return {w: v / norm for w, v in acc.items()}

    @staticmethod
    def sim(a, b):
        if len(b) < len(a):
            a, b = b, a
        return sum(v * b.get(w, 0.0) for w, v in a.items())


class DenseRep:
    """Shared shape for dense encoders: numpy vectors, dot-product similarity on
    normalized embeddings."""

    def __init__(self, name, encode_batch):
        self.name = name
        self._encode_batch = encode_batch

    def fit(self, train_texts):
        pass

    def encode(self, text):
        return self._encode_batch([text])[0]

    def encode_many(self, texts):
        return self._encode_batch(texts)

    @staticmethod
    def centroid(vecs):
        import numpy as np
        m = np.mean(np.asarray(vecs), axis=0)
        n = np.linalg.norm(m) or 1.0
        return m / n

    @staticmethod
    def sim(a, b):
        return float(a @ b)


def load_model2vec():
    from model2vec import StaticModel
    m = StaticModel.from_pretrained("minishlab/potion-base-8M")

    def enc(texts):
        import numpy as np
        v = m.encode(texts)
        n = np.linalg.norm(v, axis=1, keepdims=True)
        n[n == 0] = 1.0
        return list(v / n)

    return DenseRep("model2vec", enc)


def load_minilm():
    from sentence_transformers import SentenceTransformer
    m = SentenceTransformer("sentence-transformers/all-MiniLM-L6-v2", device="cpu")

    def enc(texts):
        return list(m.encode(texts, normalize_embeddings=True, show_progress_bar=False))

    return DenseRep("minilm", enc)


# ---- the protocol (identical shape to runHDCStandard in standard_test.go) ----

def run(rep, ds):
    classes = [i["name"] for i in ds["intents"]]
    train_texts = [t for i in ds["intents"] for t in i["train"]]
    t0 = time.perf_counter()
    rep.fit(train_texts)
    prototypes = {}
    for intent in ds["intents"]:
        if hasattr(rep, "encode_many"):
            vecs = rep.encode_many(intent["train"])
        else:
            vecs = [rep.encode(t) for t in intent["train"]]
        prototypes[intent["name"]] = rep.centroid(vecs)
    train_seconds = time.perf_counter() - t0

    def route(text):
        q = rep.encode(text)
        best, best_sim = None, -1e30
        for name in classes:  # fixed order → deterministic ties
            s = rep.sim(q, prototypes[name])
            if s > best_sim:
                best, best_sim = name, s
        return best, best_sim

    gold, pred, durs, in_sims = [], [], [], []
    for intent in ds["intents"]:
        for text in intent["test"]:
            t1 = time.perf_counter()
            got, s = route(text)
            durs.append((time.perf_counter() - t1) * 1e6)
            gold.append(intent["name"])
            pred.append(got)
            in_sims.append(s)
    durs.sort()
    correct = sum(1 for g, p in zip(gold, pred) if g == p)

    res = {
        "router": rep.name,
        "dataset": ds["name"],
        "classes": len(classes),
        "test_total": len(gold),
        "accuracy": correct / len(gold),
        "macro_f1": macro_f1(gold, pred, classes),
        "p50_us": percentile(durs, 0.50),
        "p99_us": percentile(durs, 0.99),
        "train_seconds": train_seconds,
    }

    # Cold-add: hold out the alphabetically first intent (same choice as the Go side).
    held = ds["intents"][0]
    rest_protos = {k: v for k, v in prototypes.items() if k != held["name"]}
    t2 = time.perf_counter()
    if hasattr(rep, "encode_many"):
        vecs = rep.encode_many(held["train"])
    else:
        vecs = [rep.encode(t) for t in held["train"]]
    rest_protos[held["name"]] = rep.centroid(vecs)
    res["cold_add_us"] = (time.perf_counter() - t2) * 1e6
    res["cold_add_intent"] = held["name"]
    hit = 0
    for text in held["test"]:
        q = rep.encode(text)
        best, best_sim = None, -1e30
        for name in classes:
            s = rep.sim(q, rest_protos[name])
            if s > best_sim:
                best, best_sim = name, s
        if best == held["name"]:
            hit += 1
    res["cold_add_acc_after"] = hit / len(held["test"]) if held["test"] else 0.0

    # OOS AUROC (CLINC150): score = -max_similarity, so OOS (less similar) scores higher —
    # the exact analogue of the HDC side's min-distance.
    if ds.get("oos_test"):
        oos_scores = []
        for text in ds["oos_test"]:
            _, s = route(text)
            oos_scores.append(-s)
        res["oos_auroc"] = auroc(oos_scores, [-s for s in in_sims])
        res["oos_total"] = len(ds["oos_test"])

    out = HERE / f"results_{rep.name}_{ds['name']}.json"
    out.write_text(json.dumps(res, indent=2))
    print(f"=== {ds['name']} ({rep.name}): acc {100*res['accuracy']:.1f}%  "
          f"macro-F1 {res['macro_f1']:.3f}  p50 {res['p50_us']:.1f} µs"
          + (f"  oos-AUROC {res['oos_auroc']:.3f}" if "oos_auroc" in res else ""))


def main():
    if not DATASETS:
        sys.exit("no datasets/*.arena.json — run python3 arena/datasets/fetch.py first")
    wanted = sys.argv[1:] or ["tfidf", "model2vec", "minilm"]
    loaders = {"tfidf": lambda: TfidfRep(), "model2vec": load_model2vec, "minilm": load_minilm}
    for name in wanted:
        try:
            rep = loaders[name]()
        except Exception as e:  # missing dep or download failure — report, don't hide
            print(f"SKIP {name}: {e}")
            continue
        for path in DATASETS:
            ds = json.loads(path.read_text())
            run(rep, ds)


if __name__ == "__main__":
    main()
