package engine

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// certFixture builds a small trained memory plus the state/candidates of one decision.
func certFixture(t *testing.T) (*AssociativeMemory, core.Hypervector, []string) {
	t.Helper()
	dict := core.NewTokenDictionary()
	mem := NewAssociativeMemory()
	mem.SetSite(11)

	state := dict.GetOrRegister("state:alpha")
	tools := []string{"ReadFile", "WriteFile", "RunTests"}
	mem.StoreLabeled(state, dict.GetOrRegister("RunTests"), "alpha→RunTests")
	mem.StoreLabeled(dict.GetOrRegister("state:beta"), dict.GetOrRegister("ReadFile"), "beta→ReadFile")
	return mem, state, tools
}

func TestCertificateIssueVerifyRoundTrip(t *testing.T) {
	mem, state, tools := certFixture(t)

	cert, err := IssueDecision(mem, state, tools, 0)
	if err != nil {
		t.Fatal(err)
	}
	// With two lessons bundled (even N), recall is not distance-0 but must sit decisively
	// below the ~5000 noise floor with the correct token on top.
	if cert.Chosen != "RunTests" || cert.Distance > 3500 {
		t.Fatalf("expected decisive recall of RunTests, got %q at %d", cert.Chosen, cert.Distance)
	}
	if margin := cert.Candidates[1].Distance - cert.Candidates[0].Distance; margin < 500 {
		t.Fatalf("recall margin too small: %d", margin)
	}
	if len(cert.Contributors) != 1 || cert.Contributors[0].Label != "alpha→RunTests" {
		t.Fatalf("contributors = %+v", cert.Contributors)
	}

	// JSON round-trip (hex state) then verify against the same memory.
	data, err := json.Marshal(cert)
	if err != nil {
		t.Fatal(err)
	}
	var back DecisionCertificate
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.State != cert.State {
		t.Fatal("state vector did not survive JSON round-trip")
	}

	res := back.VerifyAgainst(mem)
	if !res.OK() || !res.FingerprintOK || !res.DecisionOK || res.Signed {
		t.Fatalf("unsigned round-trip verify failed: %+v", res)
	}
}

func TestCertificateSignatureAndTamper(t *testing.T) {
	mem, state, tools := certFixture(t)
	cert, err := IssueDecision(mem, state, tools, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert.Sign(priv)
	if !cert.VerifySignature() {
		t.Fatal("fresh signature did not verify")
	}
	if res := cert.VerifyAgainst(mem); !res.OK() || !res.Signed || !res.SignatureValid {
		t.Fatalf("signed verify failed: %+v", res)
	}

	// Any content tamper must break the signature AND the re-execution check.
	tampered := cert
	tampered.Chosen = "WriteFile"
	if tampered.VerifySignature() {
		t.Fatal("signature survived content tampering")
	}
	if res := tampered.VerifyAgainst(mem); res.OK() || res.DecisionOK {
		t.Fatalf("tampered certificate passed verification: %+v", res)
	}
}

// A certificate must fail cleanly against a memory whose state has moved on.
func TestCertificateDetectsMemoryDrift(t *testing.T) {
	mem, state, tools := certFixture(t)
	cert, err := IssueDecision(mem, state, tools, 0)
	if err != nil {
		t.Fatal(err)
	}

	dict := core.NewTokenDictionary()
	mem.StoreLabeled(dict.GetOrRegister("state:gamma"), dict.GetOrRegister("WriteFile"), "gamma→WriteFile")

	res := cert.VerifyAgainst(mem)
	if res.FingerprintOK {
		t.Fatal("fingerprint check missed memory drift")
	}
	if res.OK() {
		t.Fatalf("certificate verified against a drifted memory: %+v", res)
	}
}

// The certified router path must agree with the live selector and verify against the policy.
func TestSelectNextToolCertified(t *testing.T) {
	router := NewToolRouter()
	tr := NewTrajectoryTracker(router)
	tr.SetGoal("fix_bug")

	tool, trace, cert, err := tr.SelectNextToolCertified()
	if err != nil {
		t.Fatal(err)
	}
	if tool != ToolASTSearch || trace.Chosen != tool || cert.Chosen != tool {
		t.Fatalf("certified selection disagrees: tool=%q trace=%q cert=%q", tool, trace.Chosen, cert.Chosen)
	}
	if len(cert.Contributors) != 1 || cert.Contributors[0].Label != "fix_bug/step1→ASTSearch" {
		t.Fatalf("certificate contributors = %+v", cert.Contributors)
	}
	if res := cert.VerifyAgainst(router.policy); !res.OK() {
		t.Fatalf("router certificate failed verification: %+v", res)
	}

	// And it must agree with the plain selector on a fresh tracker.
	tr2 := NewTrajectoryTracker(router)
	tr2.SetGoal("fix_bug")
	if plain, _ := tr2.SelectNextTool(); plain != tool {
		t.Fatalf("certified (%q) and plain (%q) selection disagree", tool, plain)
	}
}
