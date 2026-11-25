// Command nvsa-verify checks NeuroVSA decision certificates, lesson packs, and world packs.
//
//	nvsa-verify -cert receipt.json -memory memory.bin   # re-execute a decision receipt
//	nvsa-verify -pack lessons.json                      # check a pack's signature
//	nvsa-verify -pack lessons.json -memory memory.bin   # ...and its installation status
//	nvsa-verify -world world.json                       # verify + replay a shared world pack
//
// Certificate verification is the ProofRoute promise made concrete: the verifier reloads
// the referenced memory, re-derives the candidate vectors from the vocab seed, re-executes
// the cleanup decision, and demands bit-exact agreement with the receipt. World packs are
// signature-checked and fully replayed (the RuleGarden determinism contract), printing the
// resulting world hash. Exit code 0 means every applicable check passed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/rulegarden"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nvsa-verify: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	certPath := flag.String("cert", "", "decision certificate JSON to verify (requires -memory)")
	packPath := flag.String("pack", "", "lesson pack JSON to verify")
	worldPath := flag.String("world", "", "world pack JSON to verify and replay")
	memPath := flag.String("memory", "", "memory file (v4) the certificate/pack is checked against")
	requireSig := flag.Bool("require-signature", false, "fail unsigned certificates/packs")
	flag.Parse()

	switch {
	case *certPath != "":
		if *memPath == "" {
			fail("-cert requires -memory (the memory the decision was made over)")
		}
		verifyCert(*certPath, *memPath, *requireSig)
	case *packPath != "":
		verifyPack(*packPath, *memPath, *requireSig)
	case *worldPath != "":
		verifyWorld(*worldPath, *requireSig)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

// verifyWorld checks a shared RuleGarden world pack: author signature (when present) and a
// full bounded replay — the same tamper-evidence path the browser runs on import. Replay
// itself re-verifies every signature, including packs quoted inside merge_brains events.
func verifyWorld(worldPath string, requireSig bool) {
	raw, err := os.ReadFile(worldPath)
	if err != nil {
		fail("read world pack: %v", err)
	}
	w, err := rulegarden.ImportJSON(raw)
	if err != nil {
		fail("world pack rejected: %v", err)
	}

	var p rulegarden.Pack
	if err := json.Unmarshal(raw, &p); err != nil {
		fail("parse world pack: %v", err)
	}
	signed := len(p.Signature) > 0
	fmt.Printf("world pack: %s — seed %d, %d events, %d ticks\n", worldPath, p.Seed, len(p.Events), p.Ticks)
	fmt.Printf("  signature : %s\n", sigState(signed, p.VerifySignature()))
	if signed {
		fmt.Printf("  author    : %s\n", p.SignerFingerprint())
	}
	fmt.Printf("  replay    : OK — world hash %s\n", w.Hash())

	if requireSig && !signed {
		fmt.Println("FAILED: pack is unsigned and -require-signature is set")
		os.Exit(1)
	}
	fmt.Println("VERIFIED: the pack replays deterministically" + map[bool]string{true: " and its author signature is valid.", false: " (unsigned — replay-verifiable only)."}[signed])
}

func verifyCert(certPath, memPath string, requireSig bool) {
	raw, err := os.ReadFile(certPath)
	if err != nil {
		fail("read certificate: %v", err)
	}
	var cert engine.DecisionCertificate
	if err := json.Unmarshal(raw, &cert); err != nil {
		fail("parse certificate: %v", err)
	}

	mem := engine.NewAssociativeMemory()
	if err := mem.LoadFromFile(memPath); err != nil {
		fail("load memory: %v", err)
	}

	res := cert.VerifyAgainst(mem)
	fmt.Printf("certificate: %s (engine %s)\n", certPath, cert.EngineVersion)
	if cert.Basis != "" {
		// Policy-annotated: the executed action is the meaningful one; the raw cleanup winner
		// may differ (e.g. an instinct override).
		fmt.Printf("  executed action   : %s (basis %s)\n", cert.ExecutedAction, cert.Basis)
		if cert.ExecutedAction != cert.Chosen {
			fmt.Printf("  raw cleanup winner : %s (distance %d)\n", cert.Chosen, cert.Distance)
		}
	} else {
		fmt.Printf("  chosen action     : %s (distance %d)\n", cert.Chosen, cert.Distance)
	}
	for _, ct := range cert.Contributors {
		fmt.Printf("  produced by       : %s  [%s]\n", ct.Label, ct.ID)
	}
	fmt.Printf("  signature         : %s\n", sigState(res.Signed, res.SignatureValid))
	fmt.Printf("  memory fingerprint: %s\n", okStr(res.FingerprintOK))
	fmt.Printf("  re-execution      : %s\n", okStr(res.DecisionOK))
	if res.Detail != "" {
		fmt.Printf("  detail            : %s\n", res.Detail)
	}

	if !res.OK() || (requireSig && !res.Signed) {
		os.Exit(1)
	}
	fmt.Println("VERIFIED: the memory reproduces this decision bit-for-bit.")
}

func verifyPack(packPath, memPath string, requireSig bool) {
	raw, err := os.ReadFile(packPath)
	if err != nil {
		fail("read pack: %v", err)
	}
	var pack engine.Pack
	if err := json.Unmarshal(raw, &pack); err != nil {
		fail("parse pack: %v", err)
	}

	signed := len(pack.Signature) > 0
	sigOK := pack.VerifySignature()
	fmt.Printf("pack: %q — site %d, %d entries\n", pack.Name, pack.Site, len(pack.Entries))
	fmt.Printf("  signature: %s\n", sigState(signed, sigOK))
	for _, e := range pack.Entries {
		fmt.Printf("  %d:%d  %s\n", pack.Site, e.Seq, e.Label)
	}

	okOverall := (!signed && !requireSig) || sigOK

	// Semantics check: a signature proves WHO signed; this proves the pack's claimed
	// meaning matches its vectors. RuleGarden-domain sems are re-encoded through the fixed
	// shared vocabulary and compared bit-exactly to the bound vector; unknown domains are
	// reported, never silently treated as verified.
	if !verifyPackSems(&pack) {
		okOverall = false
	}
	if memPath != "" {
		mem := engine.NewAssociativeMemory()
		if err := mem.LoadFromFile(memPath); err != nil {
			fail("load memory: %v", err)
		}
		active, removed, missing := 0, 0, 0
		ledger := make(map[engine.AssociationID]bool) // id -> removed
		for _, rec := range mem.Ledger() {
			ledger[rec.ID] = rec.Removed
		}
		for _, e := range pack.Entries {
			r, ok := ledger[engine.AssociationID{Site: pack.Site, Seq: e.Seq}]
			switch {
			case !ok:
				missing++
			case r:
				removed++
			default:
				active++
			}
		}
		fmt.Printf("  in memory: %d active, %d revoked, %d not installed\n", active, removed, missing)
	}

	if !okOverall {
		os.Exit(1)
	}
}

// verifyPackSems validates every sem-carrying entry of a flat lesson pack and reports per
// entry. Returns false if any rulegarden-domain sem is malformed or contradicts its bound
// vector (a forged semantics/vector pair). Entries without a sem are legacy — importable
// under the label rule, but noted so a reader knows they carry no verified semantics.
func verifyPackSems(pack *engine.Pack) bool {
	ok := true
	legacy := 0
	vocab := rulegarden.NewVocab()
	for _, e := range pack.Entries {
		if e.Sem == "" {
			legacy++
			continue
		}
		var probe struct {
			Domain string `json:"domain"`
		}
		if err := json.Unmarshal([]byte(e.Sem), &probe); err != nil || probe.Domain != rulegarden.SemDomain {
			fmt.Printf("  sem %d:%d : present (domain %q) — not verifiable by this tool\n", pack.Site, e.Seq, probe.Domain)
			continue
		}
		if pack.VocabSeed != rulegarden.VocabSeed {
			fmt.Printf("  sem %d:%d : rulegarden domain but foreign vocab seed %d — cannot re-encode\n", pack.Site, e.Seq, pack.VocabSeed)
			ok = false
			continue
		}
		ls, err := rulegarden.ParseLessonSem(e.Sem)
		if err != nil {
			fmt.Printf("  sem %d:%d : INVALID (%v)\n", pack.Site, e.Seq, err)
			ok = false
			continue
		}
		actHV, err := vocab.ActionHV(ls.Action)
		if err != nil {
			fmt.Printf("  sem %d:%d : INVALID (%v)\n", pack.Site, e.Seq, err)
			ok = false
			continue
		}
		if vocab.EncodePercept(ls.Percept).Bind(actHV) != e.Bound {
			fmt.Printf("  sem %d:%d : FORGED — bound vector contradicts the signed semantics (%s → %s)\n", pack.Site, e.Seq, ls.Percept, ls.Action)
			ok = false
			continue
		}
		fmt.Printf("  sem %d:%d : verified (%s → %s)\n", pack.Site, e.Seq, ls.Percept, ls.Action)
	}
	if legacy > 0 {
		fmt.Printf("  sem       : %d legacy entr%s without machine-readable semantics (label-validated at import; cannot transfer)\n",
			legacy, map[bool]string{true: "y", false: "ies"}[legacy == 1])
	}
	return ok
}

func okStr(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAILED"
}

func sigState(signed, valid bool) string {
	switch {
	case !signed:
		return "unsigned (replay-verifiable only)"
	case valid:
		return "valid"
	default:
		return "INVALID"
	}
}
