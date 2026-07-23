# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] — 2026-07-23

First public release.

### Added
- Core VSA engine over 10,000-bit hypervectors (`[157]uint64`): bind (XOR), word-level
  permute, majority-vote bundle, `POPCNT` Hamming distance, and a thread-safe item-memory
  dictionary.
- Counter-vector associative memory — O(D) per write, independent of corpus size — with
  genuine `mmap` persistence and a zero-heap read-only open path.
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

[0.1.0]: https://github.com/JGautam09/NeuroVSA/releases/tag/v0.1.0
