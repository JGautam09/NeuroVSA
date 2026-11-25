# Check selection reference

[The go-chi quickstart](../examples/check-selection/chi/README.md) builds the CLI and defines the sample policy. The commands below run from the NeuroVSA checkout. Use Go 1.22 or later.

## Policy and commands

The policy declares the complete set of check IDs the repository is willing to run.
Use `always` for required checks that apply to every change. Match repository-relative
paths; `*` does not cross `/`, while a terminal `/**` matches recursively below a literal
directory. Rules are additive. A matched rule with no check IDs explicitly covers a
path without adding work.

```json
{
  "version": 1,
  "scope": "owner/repo",
  "checks": ["unit", "api-smoke"],
  "always": ["unit"],
  "rules": [
    {"id": "api", "paths": ["api/**"], "optional": ["api-smoke"]},
    {"id": "docs", "paths": ["docs/**"]}
  ]
}
```

`scope` is optional for rules-only use and must match the receiving trust file exactly
when packs are enabled. Changes use `{"status":"M","path":"api/server.go"}`. Supported
statuses are `A`, `C`, `D`, `M`, `R`, and `T`; `C` and `R` require a distinct `old_path`.
Both paths count for matching. Required check IDs always take precedence over optional
ones. An uncovered path or unreadable Git diff makes every catalog ID required.

| Command | Purpose |
| --- | --- |
| `recommend -config FILE` | Pass either `-fixture FILE` or `-repo DIR -base REF -head REF`. Add `-format json` for machine output or `-receipt` for the replay document. |
| `compare -before FILE -after FILE -cases FILE` | Compare policy versions on the same fixtures. `-gate` exits 1 on violated candidate expectations. |
| `bench -corpus FILE` | Reproduce the offline comparison. `-selector hdc` runs the benchmark-only HDC experiment. |
| `verify -file FILE` | Replay a recommendation. Exit 1 means the parsed receipt did not verify. |
| `pack-sign`, `pack-merge` | Sign an update or revocation, and merge signed events under a local trust policy. |
| `mcp -config FILE` | Serve the read-only tools over stdio. |

Exit code 2 means the command could not complete, for example because of invalid
arguments, a malformed file or a write error. A successful recommendation that
requests the full suite still exits 0; inspect `fallback` to tell it apart. CLI JSON
input files and generated replay/pack artifacts have an 8 MiB limit.

## Verify a recommendation

```sh
go build -o ./nvsa-check ./cmd/nvsa-check
nvsa_trial=$(mktemp -d)
./nvsa-check recommend -receipt \
  -config arena/check-selection/chi-policy.json \
  -fixture examples/check-selection/chi/middleware-change.json > "$nvsa_trial/receipt.json"
./nvsa-check verify -file "$nvsa_trial/receipt.json"
```

The replay file contains normalized changes, the complete local snapshot, its SHA-256 digest, and the recommendation. The verifier rebuilds the recommendation, including any full-suite fallback and approved pack effects. A historic receipt uses its original snapshot. Provide `-expected-snapshot` with a digest recorded independently to check that the snapshot is the one you expected. The replay file itself is unsigned: verification checks reproducibility but does not authenticate who produced it, which Git changes originally occurred, or which checks ran. A receipt generated from missing Git history reproduces the fallback, and the CI runner supplies evidence of which checks ran. Review the contained paths, policies and signed examples before sharing a receipt.

## Share optional recommendations

An example pack has an `owner/repo` scope, a name and a positive revision. It contains exact sets of changed paths and statuses plus optional check IDs. The receiving repo approves the signer's public key and usable check IDs in its own trust file. On import, the CLI checks that the repository scope matches and the Ed25519 signature covers the payload. Local rules remain authoritative for requirements, and current catalog membership controls which imported optional checks are eligible. Pack suggestions apply only to covered, non-fallback changes.

The sample below creates a trial policy without the local middleware suggestion so the pack's contribution is visible. It also demonstrates merging two histories. Obtain the public key from the separate key-generation output and keep the key backup private.

```sh
go build -o "$nvsa_trial/nvsa-pack" ./cmd/nvsa-pack
"$nvsa_trial/nvsa-pack" keygen -out "$nvsa_trial/signer.json"
printf 'Paste the public key from keygen: '
read -r nvsa_trial_public_key
jq -n --arg pub "$nvsa_trial_public_key" \
  '{version:1,scope:"go-chi/chi",signers:[{public_key:$pub,checks:["middleware-unit"]}]}' \
  > "$nvsa_trial/trust.json"
jq '(.rules[] | select(.id == "middleware")) |= del(.optional)' \
  arena/check-selection/chi-policy.json > "$nvsa_trial/policy.json"

./nvsa-check pack-sign -key "$nvsa_trial/signer.json" \
  -payload examples/check-selection/chi/pack-lesson.json > "$nvsa_trial/lesson.json"
./nvsa-check pack-sign -key "$nvsa_trial/signer.json" \
  -payload examples/check-selection/chi/pack-revoke.json > "$nvsa_trial/revoke.json"
./nvsa-check pack-merge -trust "$nvsa_trial/trust.json" \
  -input "$nvsa_trial/lesson.json" > "$nvsa_trial/active.json"
./nvsa-check pack-merge -trust "$nvsa_trial/trust.json" \
  -input "$nvsa_trial/revoke.json" > "$nvsa_trial/peer.json"
./nvsa-check pack-merge -trust "$nvsa_trial/trust.json" \
  -input "$nvsa_trial/active.json" -input "$nvsa_trial/peer.json" > "$nvsa_trial/merged.json"

for nvsa_trial_bundle in active merged; do
  ./nvsa-check recommend -config "$nvsa_trial/policy.json" \
    -fixture examples/check-selection/chi/middleware-change.json \
    -trust "$nvsa_trial/trust.json" -bundle "$nvsa_trial/$nvsa_trial_bundle.json"
done
```

Both recommendations require `ci-matrix`. The active bundle adds `middleware-unit`; the merged bundle drops it. Revisions are positive decimal strings in JSON. The highest revision per signing key and pack name supplies the active examples. A conflicting highest revision or competing pack suggestions with different check sets produce a conflict and withhold imported suggestions for the case. A signed revocation permanently retires that key's pack name. Re-merging an old bundle cannot reactivate it.

The receiver validates every event, including inactive history. If you remove a signer
or narrow its allowlist, recreate the input bundle from events the new trust file still
accepts; the CLI rejects an invalid bundle rather than silently ignoring events. Keep
the applicable revocations when rebuilding shared history. Exported receipts retain
their historical snapshots and require separate retention decisions. The merge format
is local and file based.

## Connect an MCP client

Configure the client to launch the built binary with the arguments below. Use absolute paths and pass `-trust` and `-bundle` together to enable approved packs for that process.

```text
/absolute/path/nvsa-check mcp -config /absolute/path/nvsa-check.json
```

The local [stdio adapter](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/stdio) supports modern `2026-07-28` discovery and the `2025-11-25` initialization sequence. It exposes two read-only tools. `recommend` returns check IDs, matching rules and fallback details. `explain` returns the same decision plus the normalized input and snapshot digest. Both take `{"changes":[{"status":"M","path":"middleware/throttle.go"}]}`. Tool paths are labels only; the adapter does not read those files, run commands, or make network calls. It loads its configured snapshot once per process. Restart the process after changing the policy, trust file, or bundle.

## Try the browser example

Build and open [RuleGarden](../rulegarden/README.md). Expand **Check recommendation demo** and switch between **Initial pack**, **New revision**, **Conflicting revisions**, and **Revoked pack**. The required suite remains visible across the sequence. **Download check receipt** produces a replay file for `nvsa-check verify` using the same evaluator. The demo uses a public sample key and its own sample policy.
