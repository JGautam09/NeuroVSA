package core

import "testing"

// Core VSA primitive benchmarks. Run with:
//
//	go test -bench . -benchmem ./core/
//
// These back the figures in the README benchmark table.

func BenchmarkBind(b *testing.B) {
	x, y := GenerateRandom(), GenerateRandom()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.Bind(y)
	}
}

func BenchmarkPermute(b *testing.B) {
	x := GenerateRandom()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.Permute(1)
	}
}

func BenchmarkBundle8(b *testing.B) {
	vs := make([]Hypervector, 8)
	for i := range vs {
		vs[i] = GenerateRandom()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Bundle(vs)
	}
}

func BenchmarkHammingDistance(b *testing.B) {
	x, y := GenerateRandom(), GenerateRandom()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = HammingDistance(x, y)
	}
}
