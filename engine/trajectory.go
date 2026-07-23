package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/JGautam09/NeuroVSA/core"
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

// StandardTools is the set of tool tokens the router cleans up against.
var StandardTools = []string{
	ToolReadFile, ToolWriteFile, ToolRunTests, ToolASTSearch, ToolHTTPReq, ToolTerminal,
}

// Workflow is a named goal paired with the ordered tool sequence that accomplishes it.
type Workflow struct {
	Goal    string
	Actions []string
}

// DefaultWorkflows are seeded into every ToolRouter so routing is goal-dependent out of the
// box. Different goals begin with different tools, which is what makes SelectNextTool return
// a goal-specific action rather than noise.
var DefaultWorkflows = []Workflow{
	{Goal: "fix_bug", Actions: []string{ToolASTSearch, ToolReadFile, ToolWriteFile, ToolRunTests}},
	{Goal: "add_feature", Actions: []string{ToolReadFile, ToolWriteFile, ToolRunTests}},
	{Goal: "refactor_code", Actions: []string{ToolASTSearch, ToolReadFile, ToolWriteFile, ToolRunTests}},
	{Goal: "deploy_service", Actions: []string{ToolRunTests, ToolTerminal, ToolHTTPReq}},
	{Goal: "write_docs", Actions: []string{ToolReadFile, ToolWriteFile}},
	{Goal: "call_api", Actions: []string{ToolHTTPReq, ToolWriteFile}},
}

// ToolRouter holds the learned routing policy that maps an agent's trajectory state to its
// optimal next tool. It is built once at startup and is safe to share read-only across many
// concurrent TrajectoryTrackers — the mutable per-agent state lives in TrajectoryTracker.
//
// The policy is an associative memory of (state -> next-action) pairs: registering a workflow
// stores, for each step k, the association between the state after k actions and action k+1.
// Selecting the next tool unbinds the current state against this memory and cleans up over the
// tool vocabulary — i.e. the same VSA machinery the decoder uses, applied to agent actions.
type ToolRouter struct {
	toolDict *core.TokenDictionary
	tools    []string
	policy   *AssociativeMemory
}

// NewToolRouter builds a router with the standard tools registered and the default workflows
// seeded.
func NewToolRouter() *ToolRouter {
	dict := core.NewTokenDictionary()
	for _, t := range StandardTools {
		dict.GetOrRegister(t)
	}
	r := &ToolRouter{
		toolDict: dict,
		tools:    append([]string(nil), StandardTools...),
		policy:   NewAssociativeMemory(),
	}
	for _, wf := range DefaultWorkflows {
		r.RegisterWorkflow(wf.Goal, wf.Actions)
	}
	return r
}

// goalVector returns the base trajectory state for a goal (its item-memory vector).
func (r *ToolRouter) goalVector(goal string) core.Hypervector {
	return r.toolDict.GetOrRegister("goal:" + goal)
}

// RegisterWorkflow teaches the router a successful tool sequence for a goal by storing a
// (state -> next-action) association at every step, advancing the state exactly the way an
// agent does at runtime: state' = ρ(state) ⊗ V_action. Each association is labeled
// "<goal>/step<k>→<action>" so traces and the ledger can name the workflow step behind a
// routing decision.
func (r *ToolRouter) RegisterWorkflow(goal string, actions []string) {
	state := r.goalVector(goal)
	for i, act := range actions {
		actHV := r.toolDict.GetOrRegister(act)
		r.policy.StoreLabeled(state, actHV, fmt.Sprintf("%s/step%d→%s", goal, i+1, act))
		state = state.Permute(1).Bind(actHV)
	}
}

// predict unbinds the policy memory against a trajectory state and returns the nearest tool
// token together with its Hamming distance (lower = more confident).
func (r *ToolRouter) predict(state core.Hypervector) (string, int) {
	query := r.policy.Matrix().Bind(state)

	best := ""
	bestDist := core.Dimension + 1
	for _, tool := range r.tools {
		if d := core.HammingDistance(query, r.toolDict.GetOrRegister(tool)); d < bestDist {
			bestDist = d
			best = tool
		}
	}
	return best, bestDist
}

// TrajectoryTracker maintains one agent's mutable execution state against a shared ToolRouter.
// Each connection/session gets its own tracker so concurrent agents never share state.
type TrajectoryTracker struct {
	mu           sync.Mutex
	router       *ToolRouter
	goal         string
	CurrentState core.Hypervector
	ActionLog    []string
}

// NewTrajectoryTracker creates a per-agent tracker bound to a shared router.
func NewTrajectoryTracker(router *ToolRouter) *TrajectoryTracker {
	return &TrajectoryTracker{
		router:       router,
		CurrentState: core.ZeroHV(),
		ActionLog:    make([]string, 0),
	}
}

// Goal returns the tracker's current goal.
func (tt *TrajectoryTracker) Goal() string {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.goal
}

// SetGoal initializes the trajectory state to the goal's base vector and clears history.
func (tt *TrajectoryTracker) SetGoal(goal string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.goal = goal
	tt.CurrentState = tt.router.goalVector(goal)
	tt.ActionLog = tt.ActionLog[:0]
}

// SelectNextTool resolves the optimal next tool for the current goal and trajectory by
// unbinding the current state against the learned policy. Returns the tool and the elapsed
// selection time.
func (tt *TrajectoryTracker) SelectNextTool() (string, time.Duration) {
	start := time.Now()
	tt.mu.Lock()
	defer tt.mu.Unlock()

	tool, _ := tt.router.predict(tt.CurrentState)
	return tool, time.Since(start)
}

// RecordAction advances the trajectory state: currentState' = ρ(currentState) ⊗ V_action.
func (tt *TrajectoryTracker) RecordAction(action string) {
	tt.mu.Lock()
	defer tt.mu.Unlock()

	actionHV := tt.router.toolDict.GetOrRegister(action)
	tt.CurrentState = tt.CurrentState.Permute(1).Bind(actionHV)
	tt.ActionLog = append(tt.ActionLog, action)
}

// GetTrajectorySummary returns a readable summary of executed actions.
func (tt *TrajectoryTracker) GetTrajectorySummary() string {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return fmt.Sprintf("Goal: %q, Step Count: %d, History: %v", tt.goal, len(tt.ActionLog), tt.ActionLog)
}
