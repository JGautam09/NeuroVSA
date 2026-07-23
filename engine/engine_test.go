package engine

import (
	"path/filepath"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

func TestAssociativeMemoryAndDecoder(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	decoder := NewHDCDecoder(mem, dict)

	// Train simple sequence: "hello" -> "world"
	helloHV := dict.GetOrRegister("hello")
	worldHV := dict.GetOrRegister("world")

	mem.StoreAssociation(helloHV, worldHV)

	// Predict next token after "hello"
	pred, dist := decoder.PredictNextToken(helloHV)
	if pred != "world" {
		t.Errorf("Expected predicted token 'world', got '%s' (dist: %d)", pred, dist)
	}

	// Test disk persistence
	tmpDir := t.TempDir()
	memFile := filepath.Join(tmpDir, "memory.bin")

	err := mem.SaveToFile(memFile)
	if err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loadedMem := NewAssociativeMemory()
	err = loadedMem.LoadFromFile(memFile)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	loadedDecoder := NewHDCDecoder(loadedMem, dict)
	predLoaded, _ := loadedDecoder.PredictNextToken(helloHV)
	if predLoaded != "world" {
		t.Errorf("Expected loaded decoder prediction 'world', got '%s'", predLoaded)
	}
}

func TestTrajectoryTracker(t *testing.T) {
	router := NewToolRouter()
	tracker := NewTrajectoryTracker(router)
	tracker.SetGoal("fix_bug")

	// Sub-millisecond tool selection.
	tool, elapsed := tracker.SelectNextTool()
	if tool == "" {
		t.Errorf("Tool selection returned empty tool string")
	}
	if elapsed.Microseconds() > 5000 {
		t.Errorf("Tool selection took longer than 5ms: %v", elapsed)
	}

	tracker.RecordAction(tool)
	tracker.RecordAction(ToolReadFile)
	if len(tracker.ActionLog) != 2 {
		t.Errorf("Expected 2 actions logged, got %d", len(tracker.ActionLog))
	}
	t.Logf("Microsecond Tool Selection Execution Time: %v (Selected Tool: %s)", elapsed, tool)
}

// Routing must depend on the goal: each seeded workflow's first tool is recovered from its
// goal state, and distinct goals route to distinct first tools (not argmin-over-noise).
func TestToolRoutingIsGoalDependent(t *testing.T) {
	router := NewToolRouter()

	for _, wf := range DefaultWorkflows {
		tr := NewTrajectoryTracker(router)
		tr.SetGoal(wf.Goal)
		if got, _ := tr.SelectNextTool(); got != wf.Actions[0] {
			t.Errorf("goal %q: first tool = %q, want %q", wf.Goal, got, wf.Actions[0])
		}
	}

	a := NewTrajectoryTracker(router)
	a.SetGoal("fix_bug") // -> ASTSearch
	b := NewTrajectoryTracker(router)
	b.SetGoal("deploy_service") // -> RunTests
	ta, _ := a.SelectNextTool()
	tb, _ := b.SelectNextTool()
	if ta == tb {
		t.Errorf("distinct goals routed to the same first tool %q", ta)
	}
}

// Recording an action must advance the trajectory so successive selections walk the whole
// learned workflow — genuine history-dependent routing, not a fixed first-step guess.
func TestToolRoutingWalksWorkflow(t *testing.T) {
	router := NewToolRouter()
	tr := NewTrajectoryTracker(router)

	want := []string{ToolASTSearch, ToolReadFile, ToolWriteFile, ToolRunTests}
	tr.SetGoal("fix_bug")
	for i, w := range want {
		got, _ := tr.SelectNextTool()
		if got != w {
			t.Fatalf("fix_bug step %d: routed %q, want %q", i, got, w)
		}
		tr.RecordAction(got)
	}
}
