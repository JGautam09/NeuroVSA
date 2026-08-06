# Upgrade plan — v0.9.0 → v0.10.0

Status of the agreed upgrade program (planned 2026-08-07). Ground rules, matching the
project's culture: every feature lands with a measured gate the way G0/G2 did — no claim
without a test or benchmark behind it. Each PR is independently mergeable; changelog
entries are written with the release.

## Release map

| Release | Contents | Status |
| :-- | :-- | :-- |
| **v0.9.0** | PR-1 CI hardening · PR-2 lesson provenance (P1-4) · PR-3 verifier CLI tests · PR-4 bit-sliced Bundle | in progress |
| **v0.10.0** | PR-5 arena v2 (standard datasets) · PR-6 dimensionality study | planned |
| Parked | Tier-4 items (below) | gated |

## PR-1 — CI & supply-chain hardening

- [x] `govulncheck` job (ubuntu).
- [x] `staticcheck` job — verified clean locally first (one finding fixed: unused
  `Server.mu` field in `api/server.go`).
- [x] Weekly scheduled fuzz workflow: `FuzzPackUnmarshal`, `FuzzMemoryUnmarshalBinary`,
  and a new `FuzzHexVector` target, 5 minutes each; failing corpus uploaded as artifact.
- [x] Per-package coverage floors (~2 pts under the 2026-08 baseline: core 61.2, engine
  86.5, rulegarden 81.9, parser 78.0, api 82.7, arena 95.3). Raise floors as coverage
  rises; never lower them to make a PR pass.
- [x] `.github/dependabot.yml` (gomod, npm in /ui, github-actions).
- [x] Toolchain: verified at implementation time — latest stable is go1.26.5, so the
  existing `1.26` CI pin is already current. The 1.22 min-version job stays.
- Note: the branch-protection ruleset requires the exact `test (<os>)` contexts; new jobs
  are additive. Consider marking `govulncheck`/`staticcheck` required after they prove
  stable.

## PR-2 — Lesson provenance: close P1-4

**Problem** (docs/security/REVIEW-2026-07.md): `Brain.Transfer` recovers lesson semantics
by string-parsing the display label, but in an imported pack `Label` and `Bound` are
independent — the signature attests neither matches the other. A crafted pack can claim
"flee predator" while binding "approach food", and a Transfer launders that into a
locally-authored lesson.

**Design:**

1. Engine: `PackEntry` and the ledger entry gain an optional, domain-agnostic `Sem`
   field (canonical JSON as a string), included in `CanonicalBytes()`. Pack wire format
   bumps to v2 **additively**: v1 packs (no `Sem`) keep their exact canonical bytes, so
   all existing signed packs — including the registry demo packs — keep verifying.
2. RuleGarden schema: `{v:1, domain:"rulegarden", percept:{sees,dist,dir}, action,
   parent?}` written by `Teach`/`Transfer`; `Transfer` reads `Sem`, never the label.
   `Label` becomes display-only.
3. Enforcement invariant: on every import (`ApplyLessonPackTo`, registry import, live
   sync, replay of embedded packs), re-encode `Sem`'s percept+action through the seeded
   vocab and require bit-exact equality with `Bound`; refuse the entry otherwise.
4. Legacy packs (no `Sem`): import allowed, marked **unverified provenance** in the
   inspector; `Transfer` refuses them with a clear error.
5. Memory image bumps to v4 (ledger carries `Sem`); v3 rejected with a descriptive error
   (the v0.3.0 precedent — re-train and re-save). `Sem` participates in `Fingerprint`
   (the P1-3 lesson: never hash less than the future-relevant state).
6. `nvsa-verify` / `nvsa-pack verify` gain the same re-encode check for
   `domain:"rulegarden"`; unknown domains are reported as present-but-not-verifiable.
   Wasm bridge + UI show a verified/unverified provenance badge (zero-`innerHTML`
   invariant maintained).

**Gates:** tamper test (mismatched `Sem`↔`Bound` refused at every boundary), v1-signed
pack compatibility test, merge property tests extended, image v4 round-trip + v3
rejection, fuzz corpus extended, replay goldens regenerated (world hashes change — sem is
now hashed; documented in the changelog), review-record addendum marking P1-4 closed.

## PR-3 — `nvsa-verify` CLI tests

Refactor `main` into a testable `run(args, stdout, stderr) int` (the
`cmd/nvsa-pack/main_test.go` pattern); golden end-to-end fixtures — deterministic memory
+ receipt + world pack → expected verdicts and exit codes, including
`-require-signature` and tamper cases.

## PR-4 — Bit-sliced `Bundle`

Carry-save-adder bit-sliced majority per word-column (64 lanes in parallel), replacing
the per-set-bit counter tally (~37 µs for Bundle8, the dominant encode cost).

**The hard gate: bit-identity.** World hashes, goldens, and committed arena results
depend on `Bundle`'s exact output *including the deterministic tie-break*. Differential
test (naive vs. sliced) across random inputs for N ∈ 1…64 including even-N ties, plus a
fuzz differential; all existing goldens must pass unchanged; BENCHMARKS.md re-measured.

## PR-5 — Arena v2: standard datasets (v0.10.0)

- Datasets: CLINC150 and Banking77 via a fetch script with pinned upstream URLs +
  SHA-256 (fetch-and-verify keeps the repo lean and licensing clean); converted to the
  arena JSON schema; the curated corpus stays as a third dataset.
- Baselines: model2vec (existing), all-MiniLM-L6-v2, and TF-IDF nearest-centroid.
- Protocol unchanged (nearest centroid everywhere; only the representation varies).
  Metrics: accuracy, macro-F1, latency p50/p95, cold-add. Stretch: CLINC150 OOS via
  distance thresholding.
- `go test ./...` stays network-free: big-corpus runs behind an explicit gate; results
  JSON committed; ARENA_RESULTS.md extended per-dataset.
- Honesty note, stated up front: MiniLM will very likely widen HDC's paraphrase loss.
  That's the arena working as designed.

## PR-6 — Dimensionality study (v0.10.0)

- Build-tag D variants (`hd_d1024`/`hd_d2048`/`hd_d4096`; default 10,000) with
  `NumWords`/`LastWordMask` derived; golden tests skip at non-default D.
- Study script runs G0 capacity, arena accuracy, and op benchmarks per D; results land
  as `docs/DIMENSIONALITY.md` with a README pointer.
- Explicit non-goal: runtime-configurable D (fixed-size arrays are what make every op
  zero-allocation). One CI leg compiles + smoke-tests one alt-D tag so the tags can't
  rot.

## Tier 4 — parked, with explicit triggers

| Item | Trigger to un-park |
| :-- | :-- |
| SIMD core (AVX2/NEON or Go's experimental `simd` package) | Arena v2 shows encode latency gating a real use case, **and** a pure-Go fallback + bit-identity differential tests are part of the design |
| TURN fallback for live sync | A real user reports STUN failure; until then the documented limitation stands |
| v1.0.0 / API freeze | Pack wire v2 + memory v4 + arena v2 all shipped and stable for one full minor cycle with no format churn |

## Decisions taken (veto any time)

1. Memory image v3 → v4 with rejection (re-train and re-save), following the v0.3.0
   precedent — rather than a lossy "accept v3, empty semantics" migration.
2. Datasets fetched-and-verified, not committed — pinned SHA-256 in the repo, raw
   corpora downloaded at run time.
