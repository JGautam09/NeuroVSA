#!/usr/bin/env python3
"""Fetch and convert the standard arena datasets (CLINC150, Banking77).

Stdlib only. Downloads are pinned by SHA-256 — a hash mismatch aborts, so the converted
corpora are reproducible from this file alone. Raw and converted files land next to this
script and are gitignored; what the repo commits is this fetcher and the pins.

Usage:  python3 arena/datasets/fetch.py

Licenses (attribution — the reason these are fetched, not committed):
  - CLINC150 (clinc/oos-eval, data_full.json): CC BY 3.0, Larson et al. 2019,
    "An Evaluation Dataset for Intent Classification and Out-of-Scope Prediction".
  - Banking77 (PolyAI-LDN/task-specific-datasets): CC BY 4.0, Casanueva et al. 2020,
    "Efficient Intent Detection with Dual Sentence Encoders".
"""

import csv
import hashlib
import io
import json
import pathlib
import sys
import urllib.request

HERE = pathlib.Path(__file__).resolve().parent

SOURCES = {
    "clinc150.json": (
        "https://raw.githubusercontent.com/clinc/oos-eval/master/data/data_full.json",
        "36923c3705a59e08fe9c3883d8bc2dd966ef93e22cb78ac41171782a698d56e0",
    ),
    "banking77_train.csv": (
        "https://raw.githubusercontent.com/PolyAI-LDN/task-specific-datasets/master/banking_data/train.csv",
        "b06e26ac675513959a63135f11b94ea7786ed02da65db93a5650d8838cbc664b",
    ),
    "banking77_test.csv": (
        "https://raw.githubusercontent.com/PolyAI-LDN/task-specific-datasets/master/banking_data/test.csv",
        "d12d6e3bc4c3103966ae786dc435913c0c563dfa328f5a3646d0e62cfeeb474d",
    ),
}


def fetch(name: str) -> bytes:
    url, want = SOURCES[name]
    path = HERE / name
    if path.exists():
        data = path.read_bytes()
        if hashlib.sha256(data).hexdigest() == want:
            print(f"  {name}: cached, hash OK")
            return data
        print(f"  {name}: cached copy has a WRONG hash — refetching")
    print(f"  {name}: downloading {url}")
    with urllib.request.urlopen(url, timeout=120) as r:
        data = r.read()
    got = hashlib.sha256(data).hexdigest()
    if got != want:
        sys.exit(f"FATAL: {name} hash mismatch\n  want {want}\n  got  {got}\n"
                 "Upstream changed — verify the content before updating the pin.")
    path.write_bytes(data)
    return data


def group(pairs):
    """[[text, label], ...] -> {label: [texts]} with per-label input order preserved."""
    out = {}
    for text, label in pairs:
        out.setdefault(label, []).append(text)
    return out


def write_arena(name: str, description: str, license_note: str,
                train_by, test_by, oos_test=None) -> None:
    intents = [
        {"name": label, "train": train_by[label], "test": test_by.get(label, [])}
        for label in sorted(train_by)
    ]
    doc = {
        "name": name,
        "description": description,
        "license_note": license_note,
        "intents": intents,
    }
    if oos_test:
        doc["oos_test"] = oos_test
    out = HERE / f"{name}.arena.json"
    out.write_text(json.dumps(doc, ensure_ascii=False, indent=1))
    n_train = sum(len(i["train"]) for i in intents)
    n_test = sum(len(i["test"]) for i in intents)
    print(f"  wrote {out.name}: {len(intents)} intents, {n_train} train, {n_test} test"
          + (f", {len(oos_test)} oos" if oos_test else ""))


def main() -> None:
    print("fetching (pinned by SHA-256):")
    clinc = json.loads(fetch("clinc150.json"))
    b77_train = fetch("banking77_train.csv").decode("utf-8")
    b77_test = fetch("banking77_test.csv").decode("utf-8")

    print("converting:")
    write_arena(
        "clinc150",
        "CLINC150 full split: 150 intents x 100 train / 30 test, plus 1000 out-of-scope "
        "test utterances (val splits unused).",
        "CC BY 3.0 — Larson et al. 2019 (clinc/oos-eval)",
        group(clinc["train"]),
        group(clinc["test"]),
        oos_test=[t for t, _ in clinc["oos_test"]],
    )

    def read_csv(text):
        rows = list(csv.DictReader(io.StringIO(text)))
        return group([[r["text"], r["category"]] for r in rows])

    write_arena(
        "banking77",
        "Banking77: 77 fine-grained banking intents, official train/test split.",
        "CC BY 4.0 — Casanueva et al. 2020 (PolyAI-LDN/task-specific-datasets)",
        read_csv(b77_train),
        read_csv(b77_test),
    )
    print("done.")


if __name__ == "__main__":
    main()
