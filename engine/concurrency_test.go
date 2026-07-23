package engine

import (
	"sync"
	"testing"
)

// Many concurrent agents share one read-only ToolRouter while each mutates only its own
// TrajectoryTracker. Run under `go test -race` this asserts there is no shared mutable state
// across connections.
func TestConcurrentTrackersNoRace(t *testing.T) {
	router := NewToolRouter()
	goals := []string{"fix_bug", "deploy_service", "add_feature", "write_docs", "call_api", "refactor_code"}

	var wg sync.WaitGroup
	for g := 0; g < 12; g++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tr := NewTrajectoryTracker(router)
			tr.SetGoal(goals[idx%len(goals)])
			for i := 0; i < 25; i++ {
				tool, _ := tr.SelectNextTool()
				tr.RecordAction(tool)
			}
			_ = tr.GetTrajectorySummary()
		}(g)
	}
	wg.Wait()
}
