# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.8.1] — 2026-07-24 "Hardened"

Security & correctness hardening from an external review (2×P0, 5×P1, 2×P2 — all verified
against the code, the two P0s and the deadlock reproduced before fixing). Full verification
record in [`docs/security/REVIEW-2026-07.md`](docs/security/REVIEW-2026-07.md).

### Security
- **P0 — stored XSS could exfiltrate the browser signing key.** Untrusted pack labels
  (lesson/contributor) were interpolated into `innerHTML`; a `<img onerror>` payload could
  execute and read the ed25519 seed from IndexedDB. Fixed: all rendering rebuilt with DOM
  `createElement` + `textContent` (an `el()` helper), **zero** `innerHTML` in the UI, plus a
  strict **Content-Security-Policy** (`script-src 'self' 'wasm-unsafe-eval'`). Proven inert
  in-browser against HTML/quote/SVG/event-handler payloads; a dependency-free CI check
  (`web/rulegarden/security.test.mjs`) keeps the invariant from regressing.
- **P0 — a non-canonical hypervector crashed the engine.** External hex/binary decoders
  accepted the final word's unused high 48 bits; one set bit indexed the `[10000]uint32`
  vote-counter array out of range, panicking Go/wasm (reproduced from a pack JSON). Fixed:
  `core.Hypervector.ValidateCanonical` **rejects** (never masks — masking would alter signed
  bytes) at every untrusted boundary — hex decode, pack materialization, memory-image loader,
  certificate state. Fuzz tests for the pack and memory-image parsers.
- **P1 — live sync sent the brain before identity approval.** The snapshot broadcast on
  channel-open, before the trust prompt. Replaced with a **mutual-approval handshake**
  (hello → local approval → `accept` → both-ready); no snapshot or mutation is sent or
  applied until both sides approve, and once a peer advertises a key every pack from that
  connection must be signed by it (unsigned/mismatched refused). Verified in-browser.

### Fixed
- **P1 — opposite-direction concurrent `Merge` could deadlock** (`A.Merge(B)` ∥ `B.Merge(A)`
  each held its source read-lock while waiting for the other's write-lock). `Merge` now
  snapshots the source ledger under a single read lock, releases it, then takes only the
  destination lock — no two-lock hold. Regression test: 200× opposite-direction merges +
  fan-in under `-race`.
- **P1 — `World.Hash()` omitted future-relevant state** (bound vectors, vocab seed, writer
  site/seq), so two worlds could hash equal yet diverge on a later forget/merge. Now hashes a
  complete `engine.AssociativeMemory.LocalStateFingerprint` per creature.
- **P1 — "generalization" could be reported with no identifiable source.** A decisive-margin
  winner with no exact or near contributor was labeled generalization; it is now `instinct`
  (both in `Brain.Decide` and the certificate's `DerivePolicyOutcome`). Contributors carry
  their Hamming `Distance`, and certificates record the `GeneralizationRadius`, so a verifier
  **re-derives** a generalization from its named sources.
- **P2 — remaining JS-unsafe `uint64` values** (`PackEntry.Seq`, RuleGarden world `Seed`,
  certificate `VocabSeed`, state-response seed) now use one shared quoted-string wire type
  (`core.QuotedU64`); a test drives an actual JSON number→`float64`→JSON cycle to prove it.
- **P2 — `MaxStmtStream` did not bound AST traversal** (`ast.Inspect`'s `return false` prunes
  children only, not siblings). Replaced with a manual visitor carrying a hard global node
  budget that terminates traversal; a pathological 5,000-statement function now encodes in
  bounded work.

### Changed
- `engine.Version` is `0.8.1` (was a stale `0.3.0-dev` baked into every signed certificate).
  Existing receipts still verify — each carries its own recorded version.
- CI adds a **Go 1.22 minimum-version** job (the version go.mod claims) and a **web UI build +
  security-check** job (React build + the XSS invariant test). Lesson labels remain
  human-readable; RuleGarden lesson provenance is a follow-up (see the review record).

## [0.8.0] — 2026-07-24 "Pair by Room Code"

### Added
- **`/signal` room-code auto-connect for live sync** (the piece Phase C deferred): a tiny
  WebSocket relay on the api server pairs exactly two peers per room and ferries the
  **same opaque base64 blobs** the manual copy/paste flow uses — nothing else. Privacy
  stated plainly: a signaling server necessarily sees SDP connection metadata, never world
  data; the relay is dropped the moment the peer channel opens, and **manual paste remains
  the serverless default**. Bounded (64 KiB frames, 256 rooms, TTL sweep, room-code
  validation) and covered by relay tests (pairing, verbatim both-way forwarding,
  room-full refusal, code reuse after teardown, oversized-frame disconnect); same
  loopback-only origin policy as `/ws`. Browser: room + relay-URL inputs and an
  *Auto connect* button beside the manual blob flow.
- **`docs/ENGINEERING_NOTES.md`** — honest write-ups of the two live-sync findings (JS
  2^53 JSON corruption caught by signature verification; single-site snapshot
  re-attribution caught by property tests): symptom → mechanism → fix → transferable
  lesson. Linked from the README docs index.

## [0.7.0] — 2026-07-24 "Shape Over Names"

### Added
- **AST encoder v2 (Phase D — structural code encoding)**: alongside v1's identifier
  names, v2 encodes receiver/param/return **types** (role-bound: `param-name ⊗ type`),
  statement **kinds**, and a position-permuted **control-flow stream** (first 64 statement
  kinds in source order). Gated by a measured retrieval benchmark (G2 in BENCHMARKS.md):
  on a renamed-twin corpus with name-bait distractors, **v1 scores 0/6 P@1** (picks the
  name-bait every time) while **v2 scores 6/6** — structure survives renaming; names alone
  don't. Deterministic under the seeded dictionary, pinned by a SHA-256 golden CI enforces
  on ubuntu + macos; v1's exact historical output is protected by a bit-identity test.
  Select with `parser.NewCodeASTIndexerV2` or the server's `-ast-encoder` flag
  (default 2; `1` keeps legacy behavior).

## [0.6.0] — 2026-07-24 "Worlds in Contact"

### Added
- **Live world sync (trust arc, Phase C)** — two players connect their worlds over a
  **WebRTC data channel**: world data never touches a server. Signaling is manual
  copy/paste (no infrastructure; the same trust gesture as pasting a pack), STUN-only —
  the no-TURN limit is stated plainly. Selected creatures sync creature-to-creature:
  teaches/transfers broadcast as **flat, signed lesson packs**; **forgets propagate as
  revocations**; everything applies through new logged `apply_pack` / `revoke_pack` world
  events, so a live-synced world **still replays bit-exactly**. Convergence is property-
  tested (bidirectional, 3-peer seeded gossip, tombstone propagation, reconnect
  idempotence) and was proven live between two browser tabs.
- **`engine.PacksFromMemory`** — lossless multi-site snapshot: one single-site pack per
  writer site, so a brain that absorbed lessons from several authors relays each under its
  **original author's site** (never re-attributed). `rulegarden.World` gains
  `BrainPacks` / `ApplyLessonPackTo` / `RevokeLessonPackFrom` / `RevocationPack`
  (revocations carry zero bound vectors — revocation is by identity, so a revocation pack
  can never be replayed as a teach).
- Wasm bridge: `brainPacks`, `revocationPack`, `inspectLessonPack`, `applyRemotePack`,
  `applyRemoteRevoke`; browser: `sync.js` transport + a Live sync panel with a per-
  connection peer-identity confirm (packs signed by a different key than the connected
  peer are refused).

### Fixed
- **`engine.Pack` wire format: 64-bit ids are now quoted strings in JSON.** Site ids use
  the full uint64 range, and JavaScript's JSON round-trip silently corrupts numbers above
  2^53 — which invalidated pack signatures in transit (the tamper check caught it; the
  wire format was at fault). Unmarshal still accepts bare numbers from earlier writers;
  a regression test pins the quoting.

## [0.5.0] — 2026-07-24 "The Registry Is a Pull Request"

### Added
- **Public pack registry (trust arc, Phase B)** — a **static, GitHub-native registry** of
  signed packs: `registry/index.json` manifest + `registry/packs/*`; hosting is any static
  file server and **publishing is a pull request** (no backend, no accounts). The trust
  model is explicit: the ed25519 signature embedded in each pack is authoritative; the
  manifest's sha256/author fields only catch tampering early — and `nvsa-pack verify` +
  a standing CI test lint every committed entry (hash, size, replay, signature, and
  manifest-author ↔ embedded-key agreement).
- **`cmd/nvsa-pack`** — `keygen` / `sign` / `publish` / `verify` (stdlib only). Key files
  reuse the browser's key-backup format, so a browser identity can publish from the CLI;
  world packs are fully replayed before signing — never sign what doesn't validate.
- **Browser "Pack registry" panel** — opt-in (nothing is fetched until *Load*): lists packs
  with signer fingerprints; import/merge is gated by manifest-sha256 check → engine
  signature verification → an SSH-known-hosts-style **trusted-signers** prompt on first
  contact with a signer (remembered locally). New `inspectPack` bridge call surfaces
  signature state without mutating the world.
- **Reference registry content** — two example world packs signed by a **deliberately
  public demo key** (seed committed and documented): they demonstrate the mechanism, not
  authorship — stated plainly in `registry/README.md`.

## [0.4.0] — 2026-07-24 "Signed by the Browser"

### Added
- **Browser signing (trust arc, Phase A)**: RuleGarden decision receipts and exported world
  packs are now **ed25519-signed in the browser** by a per-player identity — the signature
  comes from the same stdlib `crypto/ed25519` compiled into the wasm engine, so browser and
  CLI signatures are bit-identical (pinned by a golden-signature test). Key custody:
  generated in-engine, seed persisted in IndexedDB, export/import backup flow, fingerprint
  shown in the UI (honest XSS caveat documented — it is a game identity, not a credential).
- **Signed world packs, recursively tamper-evident**: a world pack can carry its author's
  signature over the canonical seed+event content; replay refuses a signed pack whose
  content no longer matches — at every nesting level, so tampering with a pack *quoted
  inside another world's merge event* is caught too. Unsigned packs import as before.
- **`nvsa-verify -world`**: verify a shared world pack from the CLI — author signature,
  bounded replay, and the resulting world hash (`-require-signature` for strict mode).
  Proven end-to-end: a browser-signed receipt and world pack verify natively.
- `engine.KeyFingerprint`: the short display identity (first 8 bytes of SHA-256, hex) used
  for signer chips and trusted-signer lists; signatures always verify against the full key.

### Changed
- **Relicensed from MIT to the Apache License 2.0** — adds an explicit patent grant and the
  §5 inbound-equals-outbound contribution model; `NOTICE` carries the copyright. Repo
  references (README badge, CONTRIBUTING, PR template) updated.

## [0.3.0] — 2026-07-24 "NeuroMesh + ProofRoute"

Mergeable memories (the provenance ledger becomes a true CRDT) and verifiable decisions.

### Added
- **Decision certificates (ProofRoute)**: `IssueDecision` produces a machine-checkable
  receipt — state vector, candidate vocabulary, ranked table, contributors, and the memory's
  `Fingerprint` — that any holder of the memory can RE-EXECUTE to bit-exact agreement
  (`VerifyAgainst`). Optional ed25519 signatures over a deterministic binary encoding (never
  JSON). Traced `/route` responses now carry a certificate; `SelectNextToolCertified` issues
  them for agent trackers.
- **Signed lesson packs**: a pack is a mini-replica (fixed site + sequences), so
  `ApplyPack` = a NeuroMesh merge — idempotent, deduplicating across replicas that applied
  the same pack — and `RevokePack` tombstones the unit, with revocation propagating through
  merges. `PackFromMemory` is the authoring flow; ed25519 signing; hex-vector JSON.
- **`cmd/nvsa-verify`**: CLI that verifies certificates against a memory file (signature +
  fingerprint + bit-exact re-execution) and packs (signature + installation status).
- **RuleGarden integration (the arc closes)**: creature brains claim deterministic per-world
  sites, enabling **merge brains across worlds** — logged as a replayable event that embeds
  the foreign pack (merged worlds still replay bit-exactly; failed merges are atomic no-ops).
  The creature inspector issues **downloadable decision receipts** (+ brain image) that
  `nvsa-verify` re-executes natively — demonstrated end-to-end from a browser-issued receipt.
  `MarshalBinary`/`UnmarshalBinary` expose the v3 memory image for wasm and atomic clones.
- **`Merge(other)`** — associative memories now merge like replicas: ledger union with
  monotone tombstones (forgetting propagates), content-collision detection, and a
  `MergeReport` (added/shared/tombstones/capacity warning against the measured G0 envelope).
  Commutativity, associativity, idempotence, and convergence under gossip interleavings are
  proven by property tests with bit-exact fingerprint equality.
- **`Fingerprint()`** — canonical SHA-256 over the convergent state (ledger set + tombstones
  + vocab seed, writer identity excluded); also the memory-state anchor for upcoming decision
  certificates.
- **RuleGarden**: deterministic teachable world (headless Go package, wasm bridge, browser
  page) — teach one-shot lessons, transfer by analogy, forget exactly; glass-box decisions
  with lesson/generalization/instinct bases; worlds export as seed+event-log packs with
  golden-tested replay determinism. G0 capacity gate published in BENCHMARKS.md.

### Changed
- **Breaking:** `AssociationID` is now composite `(Site, Seq)` — `"site:seq"` in JSON — so
  distinct writers can never collide; `SetSite` claims writer identity (and recomputes the
  next sequence for the chosen site, so switching to a merged-in site cannot mint a colliding
  ID).
- **Breaking:** memory file format is v3 (adds site; composite-ID ledger entries; canonical
  entry order). v1/v2 files are rejected with a descriptive error.

### Security / hardening (post-review)
- The memory loader now bounds the untrusted ledger count against the file's byte budget
  before allocating, parses into local state, and **rebuilds the tally and majority vector
  from the ledger** (never trusting the serialized copies the fingerprint doesn't cover) —
  so a malformed or matrix-tampered image cannot crash `nvsa-verify` or slip past its anchor.
- Decision certificates gained re-derivable `ExecutedAction`/`Basis` fields: RuleGarden
  receipts now certify the action actually taken (instinct overrides included), issued at
  decision time against the decision-time memory image, so teaching/forgetting afterward
  cannot make a receipt certify a different historical decision.
- World replay is bounded (pack byte size, tick horizon, event count, merge-nesting depth),
  so a pasted pack cannot freeze the browser.
- `Pack.Memory` rejects labels over the 64 KiB serialization limit.

## [0.2.0] — "Foundations"

Determinism, provenance, and exact unlearning — the three direction-agnostic foundations
identified by external review. Each closes a gap between a documented claim and the code.

### Added
- **Seeded item memory** (`core.SeededHV`, `core.NewSeededTokenDictionary`): token vectors
  derive deterministically from (seed, token) and are bit-identical across runs and machines.
  Committed golden vectors (`core/testdata/`) are verified by CI on ubuntu and macos as the
  standing proof. Seed 0 is bit-compatible with the arena encoder's original stream, so all
  committed arena results remain valid.
- **Provenance ledger + exact unlearning**: `StoreAssociation`/`StoreLabeled` return an
  `AssociationID` and record each association's bound vector and label (~1.3 KB — the price
  of identifiable memories). `RemoveAssociation(id)` exactly unlearns one association via an
  O(D) counter decrement — bit-identical to never having stored it, no retraining (~11 µs,
  zero-alloc). `Ledger`, `FindByLabel`, and `Contributors` expose provenance.
- **First-class glass-box traces**: `PredictionTrace`/`GenerationTrace` carry the symbolic
  derivation, a ranked candidate table (`core.LookupCandidates`; the WebSocket returns up to
  five prompt candidates), explicit stop reasons, and any exact-match ledger
  association(s) for the chosen result. Exposed over the WebSocket API (`"trace": true`) and
  in the terminal UI (`/trace` toggle).

### Changed
- **Breaking:** `NewTokenDictionary()` is now deterministic (seeded with `DefaultSeed`);
  the legacy crypto/rand behavior moved to `NewRandomTokenDictionary()`.
- **Breaking:** memory file format is now v2 (adds vocab seed + ledger). v1 files are
  rejected with a descriptive error; re-train and re-save.
- `StoreAssociation` gained an `AssociationID` return value (existing call sites compile
  unchanged).

## [0.1.0] — 2026-07-23

First public release.

### Added
- Core VSA engine over 10,000-bit hypervectors (`[157]uint64`): bind (XOR), word-level
  permute, majority-vote bundle, `POPCNT` Hamming distance, and a thread-safe item-memory
  dictionary.
- Counter-vector associative memory — O(D) per write, independent of corpus size — with
  `mmap` persistence and a read-only open path that skips the tally and ledger.
- Go AST → hypervector structural encoder.
- Agent trajectory router: a learned (state → next-action) policy with goal- and
  history-dependent tool selection (~1 µs).
- WebSocket API with per-connection agent state; React terminal UI.
- The arena (`arena/`): an honest, reproducible HDC-vs-neural routing benchmark (Go + Python),
  with committed reference results and a crossover report.
- Benchmark harness + `BENCHMARKS.md`; OSS scaffolding (CI, SECURITY, CONTRIBUTING, Code of
  Conduct, issue/PR templates).

### Security
- Confined the `/ast` indexer to a configurable `-index-root`, rejecting absolute paths and
  `..` traversal, with a file-count cap (`parser.MaxIndexFiles`).
- WebSocket origin check defaults to loopback-only; `-allow-all-origins` to opt out.

### Changed
- Honest repositioning: the README leads with measured strengths **and** limitations (citing
  the arena) instead of unbacked "breakthrough" claims.
- Module path is now `github.com/JGautam09/NeuroVSA` (go-get-able).

[0.8.1]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.8.1
[0.8.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.8.0
[0.7.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.7.0
[0.6.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.6.0
[0.5.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.5.0
[0.4.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.4.0
[0.3.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.3.0
[0.1.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.1.0
