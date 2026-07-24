# Engineering notes — findings worth remembering

Honest write-ups of defects found during development: what happened, why, the fix, and the
transferable lesson. Both entries below came out of the trust arc's live-sync work (Phase C,
v0.6.0), and both share a meta-lesson: **the project's own guardrails — signatures and
property tests — converted silent corruption into loud, diagnosable failures.** That is the
payoff of proof-backed development, and it is why the guardrails exist.

---

## 1. JavaScript JSON silently corrupts 64-bit integers — signatures caught it

**Symptom.** First live run of two-tab world sync: every lesson pack arriving over the
WebRTC channel was refused with `pack signature INVALID — dropped`. Signing looked correct
on the sender; verification failed on the receiver. Same code, same key, same content —
apparently.

**Mechanism.** `engine.Pack.Site` is a writer identity drawn from the **full uint64 range**
(RuleGarden derives it by hashing `(world seed, creature id)` — e.g.
`6726813379716463531`). The pack's wire form carried it as a bare JSON number. JavaScript
has no 64-bit integers: `JSON.parse` coerces numbers to IEEE-754 doubles, exact only up to
2^53. The browser's `parse → stringify` hop turned `…531` into `…616` — silently. The
ed25519 signature is computed over a canonical **binary** encoding of the true value, so the
receiver's re-encoding of the corrupted JSON no longer matched: verification failed,
*correctly*. The wire format was at fault, not the crypto.

**Fix** (`engine/pack.go`, v0.6.0): 64-bit fields (`site`, `vocab_seed`) marshal as
**quoted decimal strings**; unmarshal tolerates bare numbers from pre-fix writers. The
regression test (`TestPackWireJSONSurvivesJavaScript`) pins the literal quoting in the
output — not just a Go round-trip, which could never catch this (Go preserves uint64
perfectly; only a cross-runtime hop corrupts).

**Lessons.**
- Never put full-range 64-bit integers into JSON numbers if JavaScript may ever touch the
  payload. Quote them. (The same latent risk exists for any `uint64` you expose; audit wire
  formats when a new runtime joins the path.)
- A same-language round-trip test proves nothing about a cross-runtime wire. Pin the
  **serialized form itself** (the test asserts the quoted string appears in the bytes).
- Signatures over canonical binary encodings turn silent data corruption into loud refusal.
  The "confusing" failure was the system working: without the signature, the corrupted site
  id would have been **accepted**, and two players' worlds would have quietly diverged with
  mis-attributed lessons.

---

## 2. A single-site snapshot API silently re-attributes merged data — a property test caught it

**Symptom.** The live-sync convergence test failed on its first run:
`apply_pack: pack "sync" has duplicate entry seq 1`.

**Mechanism.** `engine.Pack` is *by design* a single-site mini-replica: one `Site`, entries
keyed by `Seq` under that site. That design is exactly right for its original job —
authoring a lesson pack — and it is what makes applying a pack idempotent across replicas.
But live sync needed a snapshot of a **merged brain**, which holds lessons from *several*
writer sites. `PackFromMemory` exported every active entry under the memory's **own** site,
discarding each entry's original site. Two lessons from different authors that both had
`seq 1` collided — the error we saw. The scarier failure mode is the one that **doesn't**
error: with non-colliding seqs, foreign lessons would have been silently **re-attributed**
to the local site — corrupting provenance and breaking cross-replica deduplication — and
nothing would have complained. The duplicate-seq crash was luck, not protection.

**Fix** (`engine/pack.go`, v0.6.0): `PacksFromMemory` — the lossless snapshot form — groups
active entries **by writer site** and emits one single-site pack per site, in canonical
order. `rulegarden.World.BrainPacks` uses it, so a brain that absorbed a friend's lessons
relays them under the **friend's** site, never re-attributed. `PackFromMemory` remains the
single-author authoring flow, documented as lossless only for single-site memories.

**Lessons.**
- When an API's type system encodes an invariant ("a pack has one site"), check every new
  caller against it. The invariant was right; the new use ("snapshot a merged replica") was
  outside it, and the compiler cannot see that.
- Losslessness is a property to **test**, not assume: the convergence suite now asserts
  bit-equal fingerprints *and* that every lesson retains its original author's site after
  relay through a third party.
- Prefer designs where the dangerous case fails loudly. Here it mostly didn't — the crash
  was incidental. The property test (`TestLiveSyncConvergence`,
  `TestLiveSyncGossipConvergence`) is what actually closes the hole, because it checks the
  *semantic* outcome (provenance preserved, replicas converge), not the absence of a crash.

---

*Both fixes shipped in v0.6.0 with regression tests; see `CHANGELOG.md` and the tests named
above. Candidates for future entries belong here whenever a finding generalizes beyond its
one-line fix.*
