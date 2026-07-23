package engine

import (
	"fmt"
	"sync"
	"time"

	"neuro-vsa/core"
)

// Standard Agentic Tool Actions
const (
	ToolReadFile  = "ReadFile"
	ToolWriteFile = "WriteFile"
	ToolRunTests  = "RunTests"
	ToolASTSearch = "ASTSearch"
	ToolHTTPReq   = "HTTPRequest"
	ToolTerminal  = "TerminalExec"
)

// TrajectoryTracker maintains microsecond agent state tracking using VSA cyclic permutations.
type TrajectoryTracker struct {
	mu           sync.RWMutex
	CurrentState core.Hypervector
	ToolDict     *core.TokenDictionary
	ActionLog    []string
	GoalVector   core.Hypervector
	RegisteredSequences map[string]core.Hypervector // Maps workflow goal string to target trajectory HV
}

// NewTrajectoryTracker creates a new agent trajectory tracker.
func NewTrajectoryTracker() *TrajectoryTracker {
	dict := core.NewTokenDictionary()

	// Register standard tools
	tools := []string{ToolReadFile, ToolWriteFile, ToolRunTests, ToolASTSearch, ToolHTTPReq, ToolTerminal}
	for _, tool := range tools {
		dict.GetOrRegister(tool)
	}

	return &TrajectoryTracker{
		CurrentState:        core.ZeroHV(),
		ToolDict:            dict,
		ActionLog:           make([]string, 0),
		RegisteredSequences: make(map[string]core.Hypervector),
	}
}

// SetGoal initializes the goal hypervector for a given workflow objective.
func (tt *TrajectoryTracker) SetGoal(goalDescription string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.GoalVector = tt.ToolDict.GetOrRegister("goal:" + goalDescription)
	tt.CurrentState = tt.GoalVector
}

// RecordAction updates current agent trajectory:
// currentState' = ρ(currentState) ⊗ V_action
func (tt *TrajectoryTracker) RecordAction(action string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	actionHV := tt.ToolDict.GetOrRegister(action)
	// Permute current state by 1 bit to encode step time, then XOR bind action
	tt.CurrentState = tt.CurrentState.Permute(1).Bind(actionHV)
	tt.ActionLog = append(tt.ActionLog, action)
}

// RegisterWorkflowSequence stores a known successful tool sequence for a workflow goal.
func (tt *TrajectoryTracker) RegisterWorkflowSequence(goal string, actions []string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	state := tt.ToolDict.GetOrRegister("goal:" + goal)
	for _, act := range actions {
		actHV := tt.ToolDict.GetOrRegister(act)
		state = state.Permute(1).Bind(actHV)
	}

	tt.RegisteredSequences[goal] = state
}

// SelectNextTool evaluates current agent trajectory state against goal/tool vectors
// and selects the optimal next tool action in sub-millisecond speed.
func (tt *TrajectoryTracker) SelectNextTool(goalHV core.Hypervector) (string, time.Duration) {
	start := time.Now()
	tt.mu.RLock()
	defer tt.mu.RUnlock()

	tools := []string{ToolReadFile, ToolWriteFile, ToolRunTests, ToolASTSearch, ToolHTTPReq, ToolTerminal}

	bestTool := ToolReadFile
	minDist := core.Dimension + 1

	// Compute candidate state for each tool: candidateState = ρ(currentState) ⊗ V_tool
	permutedState := tt.CurrentState.Permute(1)

	for _, tool := range tools {
		toolHV := tt.ToolDict.GetOrRegister(tool)
		candidateState := permutedState.Bind(toolHV)

		// Calculate Hamming distance between candidate state and goalHV
		dist := core.HammingDistance(candidateState, goalHV)
		if dist < minDist {
			minDist = dist
			bestTool = tool
		}
	}

	elapsed := time.Since(start)
	return bestTool, elapsed
}

// GetTrajectorySummary returns a readable summary of executed actions.
func (tt *TrajectoryTracker) GetTrajectorySummary() string {
	tt.mu.RLock()
	defer tt.mu.RUnlock()
	return fmt.Sprintf("Trajectory Step Count: %d, History: %v", len(tt.ActionLog), tt.ActionLog)
}
