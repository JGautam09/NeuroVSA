package engine

import (
	"encoding/json"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// Traced and untraced prediction/generation must return identical results — the untraced
// paths delegate to the same implementation, and this pins that contract.
func TestTracedEqualsUntraced(t *testing.T) {
	dict, dec := trainDemoChain()

	for _, seed := range [][]string{{"func"}, {"func", "main"}, {"if"}} {
		ctx := EncodeContext(dict, seed)

		tok, dist := dec.PredictNextToken(ctx)
		tr := dec.PredictNextTokenTraced(ctx, 0, true)
		if tr.Chosen != tok || tr.Distance != dist {
			t.Errorf("seed %v: traced (%q,%d) != untraced (%q,%d)", seed, tr.Chosen, tr.Distance, tok, dist)
		}

		seq, dists := dec.GenerateSequence(ctx, 8)
		seqT, distsT, _ := dec.GenerateSequenceTraced(ctx, 8, 3, true)
		if !equalStrings(seq, seqT) {
			t.Errorf("seed %v: traced sequence %v != untraced %v", seed, seqT, seq)
		}
		for i := range dists {
			if dists[i] != distsT[i] {
				t.Errorf("seed %v: distance[%d] traced %d != untraced %d", seed, i, distsT[i], dists[i])
			}
		}
	}
}

// The trace must carry exact, verifiable content: a sorted candidate table whose head is the
// chosen token, and — via the ledger — the precise labeled association that produced it.
func TestTraceContentOnDemoChain(t *testing.T) {
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	dec := NewHDCDecoder(mem, dict)

	for _, tok := range []string{"func", "main", "fmt.Println", "return", "nil"} {
		dict.GetOrRegister(tok)
	}
	prev := dict.GetOrRegister("func")
	labels := []string{"demo→main", "demo→fmt.Println", "demo→return", "demo→nil"}
	for i, tok := range []string{"main", "fmt.Println", "return", "nil"} {
		mem.StoreLabeled(prev, dict.GetOrRegister(tok), labels[i])
		prev = prev.Permute(1).Bind(dict.GetOrRegister(tok))
	}

	ctx := EncodeContext(dict, []string{"func"})
	tr := dec.PredictNextTokenTraced(ctx, 0, true)

	if tr.Chosen != "main" {
		t.Fatalf("chosen = %q, want main", tr.Chosen)
	}
	if tr.MemoryTotal != 4 {
		t.Errorf("memory_total = %d, want 4", tr.MemoryTotal)
	}
	if len(tr.Candidates) != dict.Size() {
		t.Errorf("candidate table has %d rows, want full dictionary (%d)", len(tr.Candidates), dict.Size())
	}
	for i := 1; i < len(tr.Candidates); i++ {
		if tr.Candidates[i].Distance < tr.Candidates[i-1].Distance {
			t.Fatalf("candidates not sorted ascending at row %d", i)
		}
	}
	if tr.Candidates[0].Token != tr.Chosen || tr.Candidates[0].Distance != tr.Distance {
		t.Errorf("candidate head (%q,%d) disagrees with chosen (%q,%d)",
			tr.Candidates[0].Token, tr.Candidates[0].Distance, tr.Chosen, tr.Distance)
	}
	if len(tr.Contributors) != 1 || tr.Contributors[0].Label != "demo→main" {
		t.Fatalf("contributors = %+v, want exactly the demo→main association", tr.Contributors)
	}

	// Generation stop reason: the chain ends by noise floor on this vocabulary.
	_, _, gt := dec.GenerateSequenceTraced(ctx, 32, 3, false)
	if gt.StopReason != StopNoiseFloor {
		t.Errorf("stop reason = %q, want %q", gt.StopReason, StopNoiseFloor)
	}
	if last := gt.Steps[len(gt.Steps)-1]; last.StopReason != StopNoiseFloor {
		t.Errorf("final step stop reason = %q, want %q", last.StopReason, StopNoiseFloor)
	}
}

// Routing traces must name the workflow step that produced the decision, and traced routing
// must agree with the untraced selector.
func TestRouterTraced(t *testing.T) {
	router := NewToolRouter()
	tr1 := NewTrajectoryTracker(router)
	tr1.SetGoal("fix_bug")

	tool, trace := tr1.SelectNextToolTraced()
	if tool != ToolASTSearch {
		t.Fatalf("traced routing chose %q, want %q", tool, ToolASTSearch)
	}
	tr2 := NewTrajectoryTracker(router)
	tr2.SetGoal("fix_bug")
	if untraced, _ := tr2.SelectNextTool(); untraced != tool {
		t.Errorf("traced (%q) and untraced (%q) selectors disagree", tool, untraced)
	}
	if len(trace.Candidates) != len(StandardTools) {
		t.Errorf("tool table has %d rows, want %d", len(trace.Candidates), len(StandardTools))
	}
	if len(trace.Contributors) != 1 || trace.Contributors[0].Label != "fix_bug/step1→ASTSearch" {
		t.Fatalf("contributors = %+v, want exactly fix_bug/step1→ASTSearch", trace.Contributors)
	}

	// Advance and confirm the second step's provenance follows.
	tr1.RecordAction(tool)
	tool2, trace2 := tr1.SelectNextToolTraced()
	if tool2 != ToolReadFile {
		t.Fatalf("step 2 chose %q, want %q", tool2, ToolReadFile)
	}
	if len(trace2.Contributors) != 1 || trace2.Contributors[0].Label != "fix_bug/step2→ReadFile" {
		t.Fatalf("step-2 contributors = %+v, want fix_bug/step2→ReadFile", trace2.Contributors)
	}
}

func TestTraceJSONRoundTrip(t *testing.T) {
	dict, dec := trainDemoChain()
	tr := dec.PredictNextTokenTraced(EncodeContext(dict, []string{"func"}), 3, true)

	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	var back PredictionTrace
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Chosen != tr.Chosen || back.Distance != tr.Distance ||
		len(back.Candidates) != len(tr.Candidates) || len(back.Contributors) != len(tr.Contributors) {
		t.Errorf("trace did not survive JSON round-trip: %+v vs %+v", back, tr)
	}
}
