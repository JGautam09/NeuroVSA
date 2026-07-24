# NeuroVSA

> A from-scratch, dependency-free **Hyperdimensional Computing (HDC / Vector Symbolic Architecture)** engine in Go — deterministic, CPU-only, no external ML dependencies.

[![CI](https://github.com/JGautam09/NeuroVSA/actions/workflows/ci.yml/badge.svg)](https://github.com/JGautam09/NeuroVSA/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![Go Report Card](https://goreportcard.com/badge/github.com/JGautam09/NeuroVSA)](https://goreportcard.com/report/github.com/JGautam09/NeuroVSA)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

NeuroVSA implements the core VSA algebra — **bind** (XOR), **bundle** (majority vote), **permute** (cyclic rotate), and **Hamming-distance cleanup** — over 10,000-bit binary hypervectors packed into `[157]uint64`. On top of the math core it adds an associative memory with memory-mapped persistence, a Go-AST structural encoder, an agent trajectory router, and a WebSocket API with a retro React terminal UI. Every operation is integer, bitwise, and fully local.

> **Formerly "PhoneForge."** This is a research and learning project, not a production system.

---

## What it is good for — and what it isn't

HDC is **not** a drop-in replacement for neural networks or LLMs, and this README won't pretend otherwise. HDC shines where you need **determinism, auditability, a tiny footprint, and instant one-shot learning over structured inputs**. It has **no semantic generalization** — it matches surface form, not meaning.

To keep the claims honest, the repo ships [**the arena**](arena/) — a head-to-head benchmark against a tiny neural embedding router on the *same* task with the *same* algorithm (only the representation differs). The measured result, on one Apple M5 Pro:

| Axis | NeuroVSA (HDC) | model2vec (static CPU embedding) | Winner |
| :--- | :--- | :--- | :--- |
| Canonical-phrasing accuracy | 100% | 100% | tie |
| **Paraphrase accuracy** | **37.8%** | **64.4%** | **Neural** |
| Latency p50 (encode + route) | 168.1 µs | 25.0 µs | Neural |
| Cold-add a new class | 1,172 µs | 206 µs | Neural |
| **Bit-exact & portable prototypes** | **yes** | no | **HDC** |
| **Model artifact required** | **none (~KB of Go)** | yes (downloaded model) | **HDC** |

**Read it straight:** a small semantic embedding router beats HDC on paraphrase understanding, raw latency, and cold-add for this task. HDC's genuine wins are **bit-exact cross-machine determinism** and **zero model artifact**. So NeuroVSA is the right tool where inputs are **bounded/canonical** and the value is **determinism, auditability, or no-dependency deployment** — not open-vocabulary language understanding. Full method and caveats: [`arena/README.md`](arena/README.md) · [`arena/ARENA_RESULTS.md`](arena/ARENA_RESULTS.md).

---

## The flagship trio — RuleGarden, NeuroMesh & ProofRoute

Those genuine strengths aren't abstract — they ship as three composed capabilities, built as
**one product line, not three demos**. Every claim below is backed by a test, not a promise:

| Capability | What it is | Why it's real |
| :-- | :-- | :-- |
| **RuleGarden** | A deterministic, teachable artificial-life world with glass-box VSA brains, running **entirely in a browser tab** (Go → WASM — no model file, no server). Teach a creature by example, transfer a lesson by analogy, forget it *exactly*. | A world **is** its `seed + event log`; the same pack replays to a **bit-identical world hash**, golden-tested in CI on Linux *and* macOS. |
| **NeuroMesh** | The associative memory is a **CRDT**: merge two players' worlds and your creature absorbs every lesson, each keeping its **foreign site id** in the ledger — and forgetting propagates through the merge. | Commutativity, associativity, idempotence, and convergence under gossip interleavings are **property-tested** to bit-exact fingerprints. |
| **ProofRoute** | Every decision can emit a **re-executable certificate** (state, ranked candidates, contributing lessons, memory fingerprint; optional ed25519 signature). Signed **lesson packs** carry a policy as a portable mini-replica. | Anyone holding the brain re-executes a receipt to **bit-for-bit agreement**; the `nvsa-verify` CLI re-runs a browser-issued receipt natively, and any tampering breaks *both* the signature and the re-execution. |

RuleGarden is the flagship you can touch; **NeuroMesh** and **ProofRoute** are engine
capabilities that power its headline verbs — *merge worlds*, *decision receipts* — and stand
alone as library value. Every choice is labeled **lesson / generalization / instinct** (recall,
analogy, or an honest shrug), with the causing lesson named.

```bash
sh web/rulegarden/build.sh && python3 -m http.server 8090 -d web/rulegarden
# open http://localhost:8090 — teach a creature, forget the lesson, watch the numbers not lie
```

Full guide and measured limits (finite brain capacity, merge caveats): [`rulegarden/README.md`](rulegarden/README.md).

---

## Highlights

- **Zero external ML dependencies.** Pure-Go bitwise core; the only third-party import is `gorilla/websocket` for the optional API.
- **Deterministic by default.** Token vectors derive from a seeded hash stream (`core.SeededHV`), so encodings are bit-identical across runs and machines — proven by committed golden vectors that CI verifies on both ubuntu and macos.
- **Tiny & fast.** 1,256 bytes per vector; ~76 ns bind, ~43 ns Hamming, ~286 ns permute (word-level rotate), all zero-allocation.
- **Instant learning — and exact unlearning.** Associative-memory writes are O(D) counter updates, independent of corpus size; `RemoveAssociation` exactly unlearns one stored association (an O(D) counter decrement — no retraining), leaving the memory bit-identical to never having stored it. Persistence uses `mmap`; `OpenReadOnly` loads only the vocab seed and materialized matrix into a small query-only object, skipping the tally and ledger.
- **Glass-box tracing.** Every prediction and routing decision can return its derivation as data: the symbolic ops applied, a ranked candidate table with exact Hamming distances (the WebSocket returns up to five prompt candidates), and — when an exact ledger match exists — the matching stored association(s).

---

## VSA math primitives

| Operation | Symbol | Bitwise primitive | Notes |
| :--- | :---: | :--- | :--- |
| Binding | ⊗ | XOR (`^`) | Self-inverse: `A ⊗ B ⊗ B = A`. |
| Permutation | ρ | Cyclic word-level rotate | Encodes sequence position; O(NumWords). |
| Bundling | ⊕ | Majority vote | Superposition; preserves similarity to components. |
| Similarity | d_H | `POPCNT` (`bits.OnesCount64`) | Hamming distance across 157 words. |

## Benchmarks

Measured on an Apple M5 Pro (`darwin/arm64`), zero allocations per op. Reproduce with `go test -bench . -benchmem ./...` — see [`BENCHMARKS.md`](BENCHMARKS.md).

| Metric | Value | Context |
| :--- | :--- | :--- |
| Bind / Unbind (⊗) | ≈ 76 ns | XOR across `[157]uint64` |
| Permutation (ρ) | ≈ 286 ns | word-level cyclic rotate |
| Hamming distance | ≈ 43 ns | `POPCNT` across 157 words |
| Agent tool routing | ≈ 1.03 µs | `SelectNextTool`: policy unbind + cleanup |
| Memory per vector | 1,256 bytes | fixed `157 × 8`, no heap thrashing |

---

## Quickstart

**Prerequisites:** Go 1.22+, and Node 18+ only if you want the UI.

```bash
git clone https://github.com/JGautam09/NeuroVSA.git
cd NeuroVSA

go test ./...                 # run the suite
go build -o neuro-vsa .       # build the API server
./neuro-vsa                   # serves ws://localhost:8080/ws (loopback-only by default)
```

Server flags: `-port` (default 8080), `-index-root` (directory the `/ast` indexer is confined to, default `.`), `-allow-all-origins` (off by default; loopback origins only).

**Terminal UI (optional):**

```bash
cd ui && npm install && npm run dev   # open http://localhost:3000
```

Commands in the web terminal: a plain prompt like `func main` (autoregressive token generation), `/route fix_bug` (agent tool routing), `/ast .` (index Go files under the index root), and `/trace` (toggle glass-box mode — every result then shows its runners-up and any exact-match stored association).

**Run the arena benchmark:**

```bash
go test ./arena/ -run TestArenaHDC -v          # HDC side
python3 -m pip install model2vec               # neural side (or: sentence-transformers)
python3 arena/neural_baseline.py
go test ./arena/ -run TestArenaReport -v       # merge → arena/ARENA_RESULTS.md
```

---

## Architecture

```
core/       VSA vector physics — bind/bundle/permute, POPCNT Hamming, seeded item memory
parser/     Go AST → hypervector structural encoder
engine/     CRDT associative memory (ledger + mmap + merge), decoder, router, certificates
rulegarden/ Deterministic teachable world (headless core; wasm bridge in cmd/, page in web/)
registry/   Static signed-pack registry (manifest + packs; publishing is a pull request)
api/        Concurrent WebSocket server (per-connection agent state, loopback origin check)
ui/         React 18 + Tailwind streaming terminal
arena/      Honest HDC-vs-neural routing benchmark (Go + Python)
cmd/        rulegarden-wasm bridge · nvsa-verify verifier · nvsa-pack registry publisher
```

Deep dives in [`docs/`](docs): [architecture](docs/architecture.md) · [developer guide](docs/developer_guide.md) · [user manual](docs/user_manual.md).

---

## Honest limitations

- **No semantic generalization.** The text encoder matches n-grams, not meaning; paraphrases of trained phrases are frequently misrouted (see the arena).
- **A tiny embedding router beats it** on paraphrase accuracy, latency, and cold-add for natural-language routing. For open-vocabulary/voice routing, use an embedding model.
- **Best fit is structured/symbolic input** — bounded command grammars, or the agent trajectory router (state + action history → next tool), where there is no language to embed.
- **Research-grade.** APIs may change; the arena dataset is small and curated (re-run on CLINC150/Banking77 for publication-grade numbers).

## Roadmap

- Bit-sliced `Bundle` (the last non-word-level primitive) to cut encode latency.
- Arena on standard datasets (CLINC150 / Banking77) and a full-MiniLM baseline.
- Optional dimensionality reduction (HDC models are typically oversized at 10,000 bits).
- AST encoder v2: role–filler encoding of parameter/return *types*, statement kinds, and
  control flow (the current encoder only captures names — see `docs/developer_guide.md`).

## Contributing & security

See [`CONTRIBUTING.md`](CONTRIBUTING.md) and [`SECURITY.md`](SECURITY.md). Issues and PRs welcome.

## License

Apache License 2.0 — see [`LICENSE`](LICENSE). Copyright 2026 Jitendra Gautam.
