// Command nvsa-verify checks NeuroVSA decision certificates and lesson packs.
//
//	nvsa-verify -cert receipt.json -memory memory.bin   # re-execute a decision receipt
//	nvsa-verify -pack lessons.json                      # check a pack's signature
//	nvsa-verify -pack lessons.json -memory memory.bin   # ...and its installation status
//
// Certificate verification is the ProofRoute promise made concrete: the verifier reloads
// the referenced memory, re-derives the candidate vectors from the vocab seed, re-executes
// the cleanup decision, and demands bit-exact agreement with the receipt. Exit code 0 means
// every applicable check passed.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/JGautam09/NeuroVSA/engine"
)

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "nvsa-verify: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	certPath := flag.String("cert", "", "decision certificate JSON to verify (requires -memory)")
	packPath := flag.String("pack", "", "lesson pack JSON to verify")
	memPath := flag.String("memory", "", "memory file (v3) the certificate/pack is checked against")
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
	default:
		flag.Usage()
		os.Exit(2)
	}
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
