//go:build !hd_d1024 && !hd_d2048 && !hd_d4096

package core

// Dimension is the total number of bits in a hypervector. 10,000 is the classical HDC
// choice and the project default; every committed golden, capacity number, and arena
// result is pinned at this dimension. The hd_d1024/hd_d2048/hd_d4096 build tags select
// smaller dimensions for the dimensionality study (docs/DIMENSIONALITY.md) — study
// builds, not supported configurations: goldens skip, and file formats embed the
// dimension so images from different builds refuse each other loudly.
const Dimension = 10000
