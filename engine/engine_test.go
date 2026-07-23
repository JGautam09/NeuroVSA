package engine

import (
	"path/filepath"
	"testing"

	"neuro-vsa/core"
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
	tracker := NewTrajectoryTracker()
	tracker.SetGoal("fix_bug")

	// Record actions
	tracker.RecordAction(ToolASTSearch)
	tracker.RecordAction(ToolReadFile)

	summary := tracker.GetTrajectorySummary()
	if len(tracker.ActionLog) != 2 {
		t.Errorf("Expected 2 actions logged, got %v", summary)
	}

	// Test sub-millisecond tool selection
	goalHV := tracker.ToolDict.GetOrRegister("goal:fix_bug")
	tool, elapsed := tracker.SelectNextTool(goalHV)

	if tool == "" {
		t.Errorf("Tool selection returned empty tool string")
	}

	if elapsed.Microseconds() > 5000 {
		t.Errorf("Tool selection took longer than 5ms: %v", elapsed)
	}
	t.Logf("Microsecond Tool Selection Execution Time: %v (Selected Tool: %s)", elapsed, tool)
}
