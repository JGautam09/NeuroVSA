# NeuroVSA pack registry

A **static, GitHub-native registry** of signed packs: [`index.json`](index.json) is the
manifest, [`packs/`](packs/) holds the pack files. There is no server and no API — the
registry is this directory, hosted wherever files are served (the copy on `main` is the
reference registry; browsers fetch it via `raw.githubusercontent.com`). **Publishing is a
pull request.**

This is the project's one explicitly network-touching feature, and it is opt-in: the
RuleGarden page only fetches a manifest when you press *Load* in the Pack registry panel.
Everything else remains fully offline.

## Trust model, stated plainly

- **The ed25519 signature embedded in each pack is authoritative.** Verification re-checks
  it against the pack's canonical content on every import — in the browser, in
  `nvsa-verify`, and in `nvsa-pack verify`.
- **The manifest is a browsing convenience, not an authority.** Its `sha256`/`bytes` catch
  file tampering early and its author fields let UIs show a signer before downloading — but
  a manifest lie is caught, because the embedded key must match the manifest's
  `author_public_key` and the signature must verify.
- **Identity is a key, trust is yours.** There is no CA and no accounts; signers are known
  by their key fingerprint (first 8 bytes of the SHA-256 of the public key, hex). The
  RuleGarden page keeps a local trusted-signers list, SSH-known-hosts style: the first time
  you import from an unknown signer you are shown the fingerprint and asked.
- **World packs must replay.** `nvsa-pack` refuses to sign or publish a world pack that
  does not replay cleanly inside the engine's bounds — never sign what doesn't validate.

## The demo key — read this

The example packs are signed by a key whose seed is **public and committed**
([`DEMO_KEY.json`](DEMO_KEY.json); seed = SHA-256 of a documented phrase). That means
**anyone can sign as the demo key** — its signatures demonstrate the *mechanism*
(tamper-evidence, verification, fingerprints) and deliberately prove nothing about
authorship. Real publishing uses a key only you hold.

## Publish a pack

```bash
go build -o nvsa-pack ./cmd/nvsa-pack

# one-time: mint your publishing identity (or reuse your browser key via "Export key")
./nvsa-pack keygen -out my-key.json          # keep this file private

# author a world in RuleGarden, "Export world", save as my-world.json, then:
./nvsa-pack publish -key my-key.json -in my-world.json \
  -name my-pack-name -desc "one line about what it teaches"

# lint the whole registry (CI runs this on every push)
./nvsa-pack verify
```

Then open a pull request adding your `registry/packs/<name>.json` and the `index.json`
update. CI re-verifies every entry; a pack that fails verification cannot merge.

## Manifest format (v1)

```json
{
  "version": 1,
  "packs": [{
    "name": "flee-predators",
    "description": "…",
    "kind": "world",
    "file": "packs/flee-predators.json",
    "bytes": 728,
    "sha256": "…",
    "author_public_key": "base64…",
    "author_fingerprint": "16 hex chars"
  }]
}
```

`kind` is `"world"` (RuleGarden seed+events pack) or `"lessons"` (an `engine.Pack`
mini-replica). Names are unique slugs; entries stay name-sorted so every republish is a
clean, reviewable diff.
