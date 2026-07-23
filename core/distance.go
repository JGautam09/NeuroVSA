package core

import (
	"math/bits"
	"runtime"
	"sync"
)

// HammingDistance calculates the number of differing bits between two hypervectors (POP_CNT).
// STRICT CONSTRAINT: Uses math/bits.OnesCount64 on native uint64 XOR results.
func HammingDistance(a, b Hypervector) int {
	dist := 0
	for i := 0; i < NumWords; i++ {
		dist += bits.OnesCount64(a.Vector[i] ^ b.Vector[i])
	}
	return dist
}

// NormalizedDistance returns the Hamming distance scaled to [0.0, 1.0].
func NormalizedDistance(a, b Hypervector) float64 {
	return float64(HammingDistance(a, b)) / float64(Dimension)
}

// CosineSimilarityApprox maps Hamming distance into an equivalent normalized cosine similarity [-1.0, 1.0].
// In binary hyperdimensional space: CosSim = 1 - 2*(HammingDistance / Dimension).
func CosineSimilarityApprox(a, b Hypervector) float64 {
	return 1.0 - 2.0*NormalizedDistance(a, b)
}

// FindNearest searches candidate hypervectors for the entry with the minimum Hamming distance to query.
// Returns candidate index and minimum distance.
func FindNearest(query Hypervector, candidates []Hypervector) (int, int) {
	if len(candidates) == 0 {
		return -1, -1
	}

	bestIdx := 0
	minDist := HammingDistance(query, candidates[0])

	for i := 1; i < len(candidates); i++ {
		d := HammingDistance(query, candidates[i])
		if d < minDist {
			minDist = d
			bestIdx = i
		}
	}

	return bestIdx, minDist
}

// BatchParallelFindNearest performs parallelized Hamming distance search across large candidate sets using Goroutines.
func BatchParallelFindNearest(query Hypervector, candidates []Hypervector) (int, int) {
	n := len(candidates)
	if n == 0 {
		return -1, -1
	}
	if n < 100 {
		return FindNearest(query, candidates)
	}

	numCPU := runtime.NumCPU()
	chunkSize := (n + numCPU - 1) / numCPU

	type match struct {
		idx  int
		dist int
	}

	results := make(chan match, numCPU)
	var wg sync.WaitGroup

	for worker := 0; worker < numCPU; worker++ {
		start := worker * chunkSize
		end := start + chunkSize
		if end > n {
			end = n
		}

		if start >= end {
			continue
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			localBestIdx := s
			localMinDist := HammingDistance(query, candidates[s])

			for i := s + 1; i < e; i++ {
				d := HammingDistance(query, candidates[i])
				if d < localMinDist {
					localMinDist = d
					localBestIdx = i
				}
			}

			results <- match{idx: localBestIdx, dist: localMinDist}
		}(start, end)
	}

	wg.Wait()
	close(results)

	bestIdx := -1
	minDist := Dimension + 1

	for res := range results {
		if res.dist < minDist {
			minDist = res.dist
			bestIdx = res.idx
		}
	}

	return bestIdx, minDist
}
