package engine

import (
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// Engine benchmarks. Run with:
//
//	go test -bench . -benchmem ./engine/

// BenchmarkStoreAssociation measures a single association write. The cost is O(Dimension)
// and independent of how many associations were already stored (the counter vector is
// updated in place), so ns/op stays flat as the loop accumulates millions of writes.
func BenchmarkStoreAssociation(b *testing.B) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	ctx := dict.GetOrRegister("ctx")
	tgt := dict.GetOrRegister("tgt")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mem.StoreAssociation(ctx, tgt)
	}
}

// BenchmarkSelectNextTool measures end-to-end agent tool routing (unbind + cleanup over the
// tool vocabulary).
func BenchmarkSelectNextTool(b *testing.B) {
	router := NewToolRouter()
	tr := NewTrajectoryTracker(router)
	tr.SetGoal("fix_bug")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tr.SelectNextTool()
	}
}
