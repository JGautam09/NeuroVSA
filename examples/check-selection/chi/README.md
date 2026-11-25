# Try NeuroVSA check selection in go-chi

The CLI recommends checks for a commit range and shows which rule matched. The example maps check IDs to [go-chi's CI workflow](https://github.com/go-chi/chi/blob/c64ed7d6635f33a8b2feaff7dcfa55677023ac7d/.github/workflows/ci.yml) and [Make targets](https://github.com/go-chi/chi/blob/c64ed7d6635f33a8b2feaff7dcfa55677023ac7d/Makefile). It runs from a NeuroVSA checkout containing `cmd/nvsa-check`. Go 1.22 or later and Git are sufficient. The saved example and benchmark run offline.

## Run the saved case

From the NeuroVSA checkout:

```sh
go build -o ./nvsa-check ./cmd/nvsa-check
./nvsa-check recommend \
  -config arena/check-selection/chi-policy.json \
  -fixture examples/check-selection/chi/middleware-change.json
```

```text
Required: ci-matrix
Optional: middleware-unit
Rule middleware matched ["middleware/request_size_test.go"]
```

## Run against a real change in go-chi

Run these commands from the same checkout. The clone supplies Git history, and the explicit revisions pin the example to [go-chi PR #1252](https://github.com/go-chi/chi/pull/1252).

```sh
chi_trial=$(mktemp -d)
git clone --no-checkout https://github.com/go-chi/chi.git "$chi_trial/chi"
./nvsa-check recommend \
  -repo "$chi_trial/chi" \
  -base 9f8adb1aabaa8a91180a821111d3f8e668d1c0bf \
  -head c64ed7d6635f33a8b2feaff7dcfa55677023ac7d \
  -config arena/check-selection/chi-policy.json
```

The output matches the saved case. `ci-matrix` refers to the existing upstream matrix. The maintainer can use `make test-middleware` or `make test-router` for focused feedback. `examples-build` is a proposed local preflight for the touched example module, such as `go build ./...` from `_examples/rest`. The CLI only returns check IDs. The example policy and benchmark labels belong to this pilot; they are not maintained by go-chi.

## Catch a rule regression

The candidate policy below incorrectly suggests a router test for a middleware change. The saved cases also cover an unrelated router change.

```sh
./nvsa-check compare -gate \
  -before arena/check-selection/chi-policy.json \
  -after examples/check-selection/chi/nvsa-check.regression.json \
  -cases examples/check-selection/chi/compare-cases.json
```

Expect exit code `1`. The report shows `router-unit` added and `middleware-unit` removed for `middleware-change`, with a missing expected suggestion and a forbidden suggestion. `unrelated-router` stays the same. Swap `-before` and `-after` to see the repair pass with exit code `0`. Omit `-gate` for a report with exit code `0` even when an expectation fails. Invalid configuration or arguments return `2`.

## Adapt it to your repository

1. Copy [the example policy](../../../arena/check-selection/chi-policy.json) as `nvsa-check.json`. Give the checks you already run stable IDs, put CI checks you always require in `always`, and add path rules for the most important directories. A rule with no check IDs explicitly covers a path that adds no checks. Overlapping rules are additive, and a required check takes precedence over an optional suggestion for the same ID.
2. Run `nvsa-check recommend -repo . -base <base-sha> -head <head-sha> -format json` from the repo. The CLI uses the merge base for committed changes, includes deleted paths and both sides of a rename, and recommends all IDs in `checks` when any path is uncovered or Git history is unavailable. The config only supports repo-relative path patterns. `*` does not cross `/`; a terminal `/**` covers a directory recursively.
3. Copy [the report-only workflow](./github-actions.yml) into `.github/workflows/` and set the repository variable `NVSA_CHECK_REF` to a reviewed, published NeuroVSA commit SHA containing the CLI. The job builds from that pinned source and uses your policy from the pull request's base commit. Keep the existing CI jobs in place and leave this report out of your required-check rules. If the first PR introduces your policy file, the summary becomes active on later PRs once the policy is on the base branch. The workflow writes to the [GitHub Actions job summary](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-commands#adding-a-job-summary).
4. Save a few cases that capture past mistakes and run `compare -gate` when the policy changes. Use `-format json` to inspect both decisions and their matched rules. To contribute a benchmark case, include its provenance, expected checks, and a short explanation of why they apply.

See [the benchmark report](../../../arena/check-selection/RESULTS.md) to reproduce the baseline comparison.
The [receipts, packs, and MCP guide](../../../docs/check-selection-replay.md) uses the same go-chi example.
