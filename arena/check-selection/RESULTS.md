# Test selection baseline results

The rules selector preserved all required checks in 57 held-out cases and recommended 143 check IDs. Running the full catalog on the same cases would select 288 IDs. That is a 50.3% reduction in selected IDs under these policies. Similarity missed seven required check IDs even after its fallback.

| Selector | Required IDs missed, before → after fallback | Fallbacks | Check IDs selected | Optional precision | Cases with a correct optional suggestion | Eligible cases with no optional suggestion |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Full suite | 0 → 0 | 0 | 288 | — | — | — |
| Explicit rules | 12 → 0 | 3 | 143 | 40/40 | 36/36 | 0 |
| Nearest example | 26 → 7 | 13 | 173 | 34/40 | 30/36 | 6 |

Required misses count missing check IDs across cases. The pre-fallback column shows the gap in the direct recommendation, including cases deliberately labeled to require the whole suite. The full-suite baseline always selects every ID, so it has no separate optional suggestion or fallback category. Optional precision counts each explicitly optional prediction. Coverage counts eligible cases with at least one correct optional suggestion. The rules selector returned zero forbidden IDs across the three held-out cases with explicit exclusions. Nearest example returned one, and full suite returned four.

## Breakdown

| Held-out cases | Size | Full suite IDs | Rules IDs / final misses | Nearest-example IDs / final misses |
| --- | ---: | ---: | ---: | ---: |
| NeuroVSA historical | 7 | 56 | 19 / 0 | 19 / 0 |
| NeuroVSA synthetic | 8 | 64 | 32 / 0 | 48 / 2 |
| go-chi historical | 33 | 132 | 70 / 0 | 82 / 1 |
| go-chi synthetic | 9 | 36 | 22 / 0 | 24 / 4 |

The corpus has 82 cases total. There are 65 historical first-parent diffs pinned by full commit ID and 17 synthetic cases. The 25 development cases provide examples for the similarity baseline. Related changes such as go-chi's `wrap_writer` and ClientIP fixes stay in the same split. The benchmark also reports development results, which include the similarity examples themselves. Every historical file list was cross-checked against its pinned Git diff. Fixture files preserve paths and statuses alongside reviewable expectations and a reason for each label.

The labels reflect test-selection policies for this pilot. `go-race` and `cross-compile` cover NeuroVSA's protected job groups. In go-chi, `ci-matrix` covers the existing upstream matrix, focused router and middleware suggestions map to the repository Make targets, and `examples-build` is a proposed preflight. Repository workflow files and expected check applicability inform the labels. This benchmark measures agreement with those labels. Results do not establish that a smaller selection would catch the same failures or save a specific amount of CI time. The test mixes two Go projects and only 36 optional-eligible holdout cases, so it also leaves the proposed 40-case HDC evaluation target open.

Sources: [NeuroVSA CI at v0.8.1](https://github.com/JGautam09/NeuroVSA/blob/6041f528662474db7ac7e937083bb4dd2c327c7b/.github/workflows/ci.yml) · [go-chi pinned CI](https://github.com/go-chi/chi/blob/c64ed7d6635f33a8b2feaff7dcfa55677023ac7d/.github/workflows/ci.yml) · [go-chi Makefile](https://github.com/go-chi/chi/blob/c64ed7d6635f33a8b2feaff7dcfa55677023ac7d/Makefile)

## Local cost

One Darwin arm64 run with Go 1.26.5, at 200 decision timings per case, measured explicit rules at **0.96 µs p50 / 7.67 µs p95** and nearest example at **0.67 µs / 3.88 µs**. These timings cover selection over loaded inputs and omit Git and JSON parsing. Full-suite selection returns a precomputed list and timed near clock resolution. In 30 fresh CLI processes for the saved go-chi fixture after a warm-up launch, wall time was **4.54 ms p50 / 5.55 ms p95** with **5.66 MiB peak child RSS**. This includes process launch and fixture and policy parsing on a warm filesystem cache. The unstripped CLI build was **3.8 MiB**. Timings are machine-specific.

## Reproduce

```sh
go build -o ./nvsa-check ./cmd/nvsa-check
./nvsa-check bench -corpus arena/check-selection/corpus.json -repeat 200
./nvsa-check bench -corpus arena/check-selection/corpus.json -repeat 200 -format json
```

The harness runs offline from the saved fixtures. Nearest example takes the best Jaccard match over directory, extension, and change-status features from the same repository's development cases. It selects the full suite if the score is below 0.5 or the best examples disagree. The rules selector selects the full suite when it sees an uncovered path. The JSON report includes cohort totals and a result, expected checks, and issues for every case. The [go-chi quickstart](../../examples/check-selection/chi/README.md) reproduces a live Git decision and a deliberate policy regression.

## HDC optional suggestion experiment

HDC trained on the existing development splits and evaluated the unchanged holdouts. It encodes directory, extension and change-status features as seeded hypervectors. It ranks optional check IDs with ProofRoute and requires both a score margin and a nearby contributor before adding a suggestion. The rules still determine required checks and fallback. The benchmark JSON includes the ProofRoute certificate wherever HDC added a check.

| Selector | Correct optional suggestions | Correct optional coverage | Required omissions after fallback |
| --- | ---: | ---: | ---: |
| Rules | 40/40 (100%) | 36/36 (100%) | 0 |
| Rules plus HDC | 40/48 (83.3%) | 36/36 (100%) | 0 |

The HDC variant added eight incorrect optional checks and gained zero percentage points of coverage. Its selection time was **18.63 µs p50 / 25.58 µs p95** on the same Darwin arm64 environment with 200 timings per case. The unchanged corpus has 36 eligible holdouts. The promotion gate requires at least 40 eligible holdouts, at least 95% observed precision, a gain of at least 10 percentage points over the strongest simple baseline and zero required omissions. HDC remains available only in the benchmark. More cases that reflect gaps in explicit rules would give a future comparison room to measure a gain.

```sh
./nvsa-check bench -corpus arena/check-selection/corpus.json -selector hdc -repeat 200
```
