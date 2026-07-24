# RuleGarden

A deterministic artificial-life world whose creatures carry **glass-box VSA brains** — the
NeuroVSA engine's flagship demo. Teach a creature by example (one shot, no training loop),
transfer a lesson by analogy, forget it *exactly*, merge brains across worlds, and download a
machine-checkable receipt for any decision. Pure integer math, no model file, runs entirely
in a browser tab.

## Run it

```bash
sh web/rulegarden/build.sh                      # builds the wasm engine (~4 MB)
python3 -m http.server 8090 -d web/rulegarden   # any static server works
# open http://localhost:8090
```

## The five verbs

| Verb | What happens | Engine mechanism |
| :--- | :--- | :--- |
| **Teach** | "When you see *predator, near, E* → *move-away*." One click, learned instantly. | One labeled association: `StoreLabeled(percept ⊗ action)` |
| **Transfer** | Reuse a lesson with a new subject (*predator→guard*) — lineage recorded. | Role substitution + re-encoding (the "dollar of Mexico" move) |
| **Forget** | Remove one lesson; behavior reverts, provably — the candidate table returns **bit-identical** to before the lesson existed. | Exact unlearning: O(D) counter decrement + tombstone |
| **Merge brains** | Paste a friend's world; your creature absorbs every lesson in it, each keeping its **foreign site id** in the ledger. | NeuroMesh CRDT merge (commutative, idempotent, tombstones propagate) |
| **Receipt** | Download a decision receipt + brain image — **ed25519-signed by your browser identity** — and verify anywhere: `nvsa-verify -cert receipt.json -memory brain.bin` re-executes the decision **bit-for-bit** (`-require-signature` for strict mode). | ProofRoute certificate anchored to the memory fingerprint |

Toggle **/trace-style inspection** by clicking a creature: the inspector shows its percept,
the ranked action table with exact Hamming distances, and *which stored lesson caused the
action*.

## Three kinds of cognition, honestly labeled

Every decision carries a **basis**:

- **lesson** — an exactly matching stored lesson fired (probe distance 0; the trace names it);
- **generalization** — no exact match, but percepts sharing roles with a taught lesson
  soft-match (~2,500 bits for 2-of-3 shared roles). Real VSA behavior, surfaced with its
  analogical source named — not hidden;
- **instinct** — nothing matched (margin at the noise floor); the creature wanders.

## The pack registry (opt-in)

The page's **Pack registry** panel browses a [static, GitHub-native registry](../registry/)
of signed packs — the default manifest URL points at this repo's `registry/index.json` on
`main`. Nothing is fetched until you press *Load* (the page is otherwise fully offline), and
every import is gated the same way: manifest `sha256` check → embedded **ed25519 signature
verification in the engine** → an SSH-known-hosts-style trust prompt the first time you see
a signer (trusted signers are remembered locally). The signature is authoritative; the
manifest is only a catalog. Publishing is a pull request — see
[`registry/README.md`](../registry/README.md) and the `nvsa-pack` CLI.

To browse the *local* registry copy while developing, serve the repo root instead:
`python3 -m http.server 8090` → open `http://localhost:8090/web/rulegarden/` and load
`/registry/index.json`.

## Live sync (P2P, no server)

Two players can connect their worlds over a **WebRTC data channel** — world data never
touches a server. Signaling is manual copy/paste of an offer/answer blob (the same trust
gesture as pasting a world pack; no signaling infrastructure), with public STUN for NAT
traversal. **Optionally**, a room code auto-connects instead: the NeuroVSA api server's
`/signal` relay ferries the *same* blobs the manual flow pastes — and nothing else. A
signaling server necessarily sees connection metadata (SDP carries addresses), but never
world data, and the relay is dropped the moment the peer channel opens. Manual paste
remains the serverless default. Once connected, the selected creatures sync **creature-to-creature**: teaches and
transfers flow as flat, signed lesson packs; **forgets propagate as revocations** — all
applied through logged `apply_pack`/`revoke_pack` world events, so a live-synced world still
replays bit-exactly. Convergence is the NeuroMesh CRDT doing its job: re-sent packs are
no-op merges, order between mutations doesn't matter, and every lesson keeps its **author's
site id** even when relayed.

Honest limits, stated plainly:

- **No TURN relay.** STUN-only WebRTC fails on some symmetric-NAT networks; if the channel
  won't open, fall back to pack exchange.
- **Peers need different world seeds** (same-seed worlds are "the same writers"; the engine
  refuses the collision atomically).
- **Forgets propagate only while connected.** The connect snapshot carries *active* lessons,
  so a forget done while apart is not replayed on reconnect — for full historical
  reconciliation including offline forgets, use **Merge brains** (world-pack paste): its
  ledger union ORs tombstones.

## Determinism contract

A world **is** its `seed + event log` — a few hundred bytes of JSON. Same pack ⇒ bit-identical
world on any machine (golden-tested in CI on Linux and macOS, including the world hash).
Merges stay replayable because the merge event embeds the foreign pack: worlds quote the
worlds they learned from.

## Honest limits

- **Brain capacity is finite and measured**: ~128 active lessons per brain is the documented
  safe envelope (`engine.RecommendedMaxActiveAssociations`; full curve in
  [BENCHMARKS.md](../BENCHMARKS.md)). Beyond it, recall degrades gracefully toward noise —
  merging many worlds into one brain will eventually blur it. This is inherent VSA
  superposition physics, stated plainly, not a bug.
- **Merge partners need different world seeds.** Brains derive their writer identity from the
  world seed; two worlds with the same seed are "the same writers," and merging their
  divergent histories is refused (atomically — the world is left untouched).
- **Generalization is bounded to the vocabulary.** Creatures generalize across shared roles
  (predator→guard), not across meanings — there is no semantic embedding anywhere in this
  system, by design (see the repo's [arena](../arena/) for why).
- **Browser keys are a game identity, not a credential.** Receipts and exported worlds are
  ed25519-signed by a per-browser key (generated in the wasm engine, seed persisted in
  IndexedDB with an export/import backup flow). IndexedDB is same-origin storage: an XSS on
  the page could read the seed. That is an acceptable trade for signing game artifacts —
  stated plainly — and hardening (WebCrypto-wrapped, non-extractable keys) is a later step
  that would not change the signature format.

## Security & hardening

Pasted world packs, downloaded receipts, and saved brain images are treated as **untrusted
input** and validated before use (findings from an initial security review are fixed here):

- **Memory files** — the loader bounds the ledger count against the file size before
  allocating, parses atomically, and rebuilds the vote tally and majority vector *from the
  ledger* (the fingerprint's source of truth), so a malformed or matrix-tampered image cannot
  crash `nvsa-verify` or slip past its state anchor.
- **Decision receipts** certify the action the creature *actually executed*, captured at
  decision time against the decision-time brain. An instinct override (raw cleanup winner ≠
  executed action) is a structured, re-derived field — not a free-form note — and teaching or
  forgetting *after* a decision cannot retarget an already-issued receipt.
- **Pasted worlds** — replay is bounded on pack byte size, tick horizon, event count, and
  merge-nesting depth, so a shared pack cannot freeze the tab. A brain-merge site collision
  (two worlds sharing a seed) is refused atomically, leaving the world untouched.
- **CRDT identity** — switching a memory's writer site recomputes its next sequence, so a
  site adopted through a merge can never mint a colliding association id; pack labels over the
  64 KiB serialization limit are rejected before they can corrupt an image.
- **Signed world packs are tamper-evident, recursively.** A world pack can carry its author's
  ed25519 signature over the canonical seed+event content; replay refuses a signed pack whose
  content no longer matches (`pack signature is invalid`), and because merges embed the
  foreign pack verbatim, the check re-runs at every nesting level — tampering with a world
  *quoted inside another world* is caught too. Unsigned packs stay importable and are
  labeled "unsigned (replay-verifiable only)"; the signature is authoritative, never the
  displayed fingerprint.
