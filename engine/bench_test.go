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

// BenchmarkRemoveAssociation measures one exact unlearning operation: decrement the entry's
// bits from the tally and rematerialize — O(Dimension), independent of corpus size. The
// memory is re-populated in batches outside the timer.
func BenchmarkRemoveAssociation(b *testing.B) {
	dict := core.NewTokenDictionary()
	ctx := dict.GetOrRegister("ctx")
	tgt := dict.GetOrRegister("tgt")

	const batch = 4096
	mem := NewAssociativeMemory()
	next := AssociationID{Seq: 1}
	avail := 0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if avail == 0 {
			b.StopTimer()
			mem = NewAssociativeMemory()
			for j := 0; j < batch; j++ {
				mem.StoreAssociation(ctx, tgt)
			}
			next, avail = AssociationID{Seq: 1}, batch
			b.StartTimer()
		}
		if err := mem.RemoveAssociation(next); err != nil {
			b.Fatal(err)
		}
		next.Seq++
		avail--
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
