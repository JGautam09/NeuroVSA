# Contributing to NeuroVSA

Thanks for your interest! This is a research and learning project — issues, discussion, and
pull requests are all welcome.

## Fork and pull-request workflow

Contributions reach `main` through pull requests only; please do not push directly to it.

1. Fork the repository, then create a focused branch from the current `main` in your fork.
2. Make one coherent change per pull request, including tests and documentation or benchmark
   updates where they are relevant.
3. Run the checks in the next section and push your branch.
4. Open a pull request against `JGautam09/NeuroVSA:main`, complete the PR template, and respond
   to review and CI feedback.

By submitting a contribution, you agree that it may be distributed under this repository's
[MIT License](LICENSE). NeuroVSA does not require a contributor license agreement or Developer
Certificate of Origin sign-off at this time.

## Development workflow

```bash
go test ./...            # run all tests
go test -race ./...      # race detector (CI gate)
go vet ./...             # static analysis
gofmt -l .               # must print nothing (CI gate)
go test -bench . ./...   # benchmarks
```

Run `gofmt -w .` and `go test -race ./...` before opening a PR — CI enforces both, plus a
cross-compile of the build-tagged `mmap` files for linux/darwin/windows.

## Running the arena benchmark

The arena (`arena/`) is a head-to-head HDC-vs-neural routing benchmark.

```bash
go test ./arena/ -run TestArenaHDC -v          # HDC side → results_hdc.json
python3 -m pip install model2vec               # neural side (or: sentence-transformers)
python3 arena/neural_baseline.py               # → results_neural.json
go test ./arena/ -run TestArenaReport -v       # merge → ARENA_RESULTS.md
```

## Guidelines

- **Keep the core dependency-free.** Only `gorilla/websocket` (for the optional API) is an
  allowed third-party import. The `core`, `engine`, and `parser` packages must stay stdlib-only.
- **New core primitives need an equivalence test** against a naive reference implementation —
  see `core/permute_test.go` (`TestPermuteMatchesNaive`, `TestBundleMatchesNaive`).
- **Be honest in docs and benchmarks.** Every performance or accuracy claim should be
  reproducible from committed code. If a change affects the arena or benchmark numbers, update
  `BENCHMARKS.md` / `arena/` accordingly.
- **Match the surrounding style.** Comments should explain *why*, not restate *what*.

## Reporting bugs / requesting features

Use the issue templates. For security issues, follow [SECURITY.md](SECURITY.md) instead of
opening a public issue.
