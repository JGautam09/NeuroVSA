package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/JGautam09/NeuroVSA/core"
)

func filledMemory(t *testing.T, seed, site uint64, n int) *AssociativeMemory {
	t.Helper()
	m := NewAssociativeMemory()
	m.SetVocabSeed(seed)
	m.SetSite(site)
	dict := core.NewSeededTokenDictionary(seed)
	for i := 0; i < n; i++ {
		ctx := dict.GetOrRegister("ctx" + string(rune('a'+i)))
		act := dict.GetOrRegister("act" + string(rune('a'+i)))
		m.StoreLabeled(ctx, act, "lesson")
	}
	return m
}

// TestMergeNoDeadlockOppositeDirections is the P1-2 regression: A.Merge(B) and B.Merge(A)
// started simultaneously must both complete. Before the snapshot-then-single-lock fix this
// deadlocked (each goroutine held its source RLock while waiting for the other's write
// lock). Run under `go test -race`; the harness timeout fails a hang.
func TestMergeNoDeadlockOppositeDirections(t *testing.T) {
	for iter := 0; iter < 200; iter++ {
		a := filledMemory(t, 42, 1, 6)
		b := filledMemory(t, 42, 2, 6)

		done := make(chan struct{})
		go func() {
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); _, _ = a.Merge(b) }()
			go func() { defer wg.Done(); _, _ = b.Merge(a) }()
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: A.Merge(B) || B.Merge(A) deadlocked", iter)
		}
	}
}

// TestMergeConcurrentFanIn stresses many writers merging into one target concurrently — the
// single-lock design must serialize cleanly with no race and a correct final total.
func TestMergeConcurrentFanIn(t *testing.T) {
	target := NewAssociativeMemory()
	target.SetVocabSeed(42)
	target.SetSite(1)

	const peers = 8
	var wg sync.WaitGroup
	for p := 0; p < peers; p++ {
		wg.Add(1)
		go func(site uint64) {
			defer wg.Done()
			src := filledMemory(t, 42, site, 4)
			if _, err := target.Merge(src); err != nil {
				t.Errorf("merge from site %d: %v", site, err)
			}
		}(uint64(p + 10))
	}
	wg.Wait()

	if got := target.Fingerprint(); got == "" {
		t.Fatal("empty fingerprint after concurrent fan-in")
	}
	// 8 peers × 4 distinct-site lessons = 32 active associations.
	if total := target.Total(); total != peers*4 {
		t.Fatalf("want %d active after fan-in, got %d", peers*4, total)
	}
}
