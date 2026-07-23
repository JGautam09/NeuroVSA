// Package rulegarden is a deterministic, teachable artificial-life world built on the
// NeuroVSA engine. Creatures carry glass-box VSA brains: lessons are taught by example
// (one-shot), transferred by analogy (role substitution), and forgotten exactly (ledger
// removal) — and every decision can name the stored lesson that produced it.
//
// Determinism contract: a world is fully defined by (seed, event log). All randomness flows
// from a splitmix64 stream seeded by the world seed; vocabulary vectors come from the fixed
// RuleGarden vocab seed (NOT the world seed) so all worlds share one vocabulary and brains
// remain mergeable across worlds. Replaying the same pack yields a bit-identical world hash
// on every platform.
package rulegarden

// rng is a splitmix64 PRNG — the same generator family used for the engine's seeded item
// memory. It is deliberately hand-rolled (5 lines) rather than math/rand so the stream is
// stable across Go versions and platforms, which the replay-determinism guarantee requires.
type rng struct {
	state uint64
}

func newRNG(seed uint64) *rng {
	return &rng{state: seed}
}

func (r *rng) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// intn returns a deterministic value in [0, n).
func (r *rng) intn(n int) int {
	return int(r.next() % uint64(n))
}
