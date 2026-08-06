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
	"io"
	"os"

	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/rulegarden"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its seams exposed for testing: argument list in, exit code out, all
// output through the writers. Exit codes: 0 = every applicable check passed, 1 = a check
// failed or an input was unreadable, 2 = usage error.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nvsa-verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	certPath := fs.String("cert", "", "decision certificate JSON to verify (requires -memory)")
	packPath := fs.String("pack", "", "lesson pack JSON to verify")
	worldPath := fs.String("world", "", "world pack JSON to verify and replay")
	memPath := fs.String("memory", "", "memory file (v4) the certificate/pack is checked against")
	requireSig := fs.Bool("require-signature", false, "fail unsigned certificates/packs")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch {
	case *certPath != "":
		if *memPath == "" {
			fmt.Fprintln(stderr, "nvsa-verify: -cert requires -memory (the memory the decision was made over)")
			return 1
		}
		return verifyCert(*certPath, *memPath, *requireSig, stdout, stderr)
	case *packPath != "":
		return verifyPack(*packPath, *memPath, *requireSig, stdout, stderr)
	case *worldPath != "":
		return verifyWorld(*worldPath, *requireSig, stdout, stderr)
	default:
		fs.Usage()
		return 2
	}
}

func fail(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, "nvsa-verify: "+format+"\n", args...)
	return 1
}

// verifyWorld checks a shared RuleGarden world pack: author signature (when present) and a
// full bounded replay — the same tamper-evidence path the browser runs on import. Replay
// itself re-verifies every signature, including packs quoted inside merge_brains events.
func verifyWorld(worldPath string, requireSig bool, stdout, stderr io.Writer) int {
	raw, err := os.ReadFile(worldPath)
	if err != nil {
		return fail(stderr, "read world pack: %v", err)
	}
	w, err := rulegarden.ImportJSON(raw)
	if err != nil {
		return fail(stderr, "world pack rejected: %v", err)
	}

	var p rulegarden.Pack
	if err := json.Unmarshal(raw, &p); err != nil {
		return fail(stderr, "parse world pack: %v", err)
	}
	signed := len(p.Signature) > 0
	fmt.Fprintf(stdout, "world pack: %s — seed %d, %d events, %d ticks\n", worldPath, p.Seed, len(p.Events), p.Ticks)
	fmt.Fprintf(stdout, "  signature : %s\n", sigState(signed, p.VerifySignature()))
	if signed {
		fmt.Fprintf(stdout, "  author    : %s\n", p.SignerFingerprint())
	}
	fmt.Fprintf(stdout, "  replay    : OK — world hash %s\n", w.Hash())

	if requireSig && !signed {
		fmt.Fprintln(stdout, "FAILED: pack is unsigned and -require-signature is set")
		return 1
	}
	fmt.Fprintln(stdout, "VERIFIED: the pack replays deterministically"+map[bool]string{true: " and its author signature is valid.", false: " (unsigned — replay-verifiable only)."}[signed])
	return 0
}

func verifyCert(certPath, memPath string, requireSig bool, stdout, stderr io.Writer) int {
	raw, err := os.ReadFile(certPath)
	if err != nil {
		return fail(stderr, "read certificate: %v", err)
	}
	var cert engine.DecisionCertificate
	if err := json.Unmarshal(raw, &cert); err != nil {
		return fail(stderr, "parse certificate: %v", err)
	}

	mem := engine.NewAssociativeMemory()
	if err := mem.LoadFromFile(memPath); err != nil {
		return fail(stderr, "load memory: %v", err)
	}

	res := cert.VerifyAgainst(mem)
	fmt.Fprintf(stdout, "certificate: %s (engine %s)\n", certPath, cert.EngineVersion)
	if cert.Basis != "" {
		// Policy-annotated: the executed action is the meaningful one; the raw cleanup winner
		// may differ (e.g. an instinct override).
		fmt.Fprintf(stdout, "  executed action   : %s (basis %s)\n", cert.ExecutedAction, cert.Basis)
		if cert.ExecutedAction != cert.Chosen {
			fmt.Fprintf(stdout, "  raw cleanup winner : %s (distance %d)\n", cert.Chosen, cert.Distance)
		}
	} else {
		fmt.Fprintf(stdout, "  chosen action     : %s (distance %d)\n", cert.Chosen, cert.Distance)
	}
	for _, ct := range cert.Contributors {
		fmt.Fprintf(stdout, "  produced by       : %s  [%s]\n", ct.Label, ct.ID)
	}
	fmt.Fprintf(stdout, "  signature         : %s\n", sigState(res.Signed, res.SignatureValid))
	fmt.Fprintf(stdout, "  memory fingerprint: %s\n", okStr(res.FingerprintOK))
	fmt.Fprintf(stdout, "  re-execution      : %s\n", okStr(res.DecisionOK))
	if res.Detail != "" {
		fmt.Fprintf(stdout, "  detail            : %s\n", res.Detail)
	}

	if !res.OK() || (requireSig && !res.Signed) {
		return 1
	}
	fmt.Fprintln(stdout, "VERIFIED: the memory reproduces this decision bit-for-bit.")
	return 0
}

func verifyPack(packPath, memPath string, requireSig bool, stdout, stderr io.Writer) int {
	raw, err := os.ReadFile(packPath)
	if err != nil {
		return fail(stderr, "read pack: %v", err)
	}
	var pack engine.Pack
	if err := json.Unmarshal(raw, &pack); err != nil {
		return fail(stderr, "parse pack: %v", err)
	}

	signed := len(pack.Signature) > 0
	sigOK := pack.VerifySignature()
	fmt.Fprintf(stdout, "pack: %q — site %d, %d entries\n", pack.Name, pack.Site, len(pack.Entries))
	fmt.Fprintf(stdout, "  signature: %s\n", sigState(signed, sigOK))
	for _, e := range pack.Entries {
		fmt.Fprintf(stdout, "  %d:%d  %s\n", pack.Site, e.Seq, e.Label)
	}

	okOverall := (!signed && !requireSig) || sigOK

	// Semantics check: a signature proves WHO signed; this proves the pack's claimed
	// meaning matches its vectors. RuleGarden-domain sems are re-encoded through the fixed
	// shared vocabulary and compared bit-exactly to the bound vector; unknown domains are
	// reported, never silently treated as verified.
	if !verifyPackSems(&pack, stdout) {
		okOverall = false
	}

	if memPath != "" {
		mem := engine.NewAssociativeMemory()
		if err := mem.LoadFromFile(memPath); err != nil {
			return fail(stderr, "load memory: %v", err)
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
		fmt.Fprintf(stdout, "  in memory: %d active, %d revoked, %d not installed\n", active, removed, missing)
	}

	if !okOverall {
		return 1
	}
	return 0
}

// verifyPackSems validates every sem-carrying entry of a flat lesson pack and reports per
// entry. Returns false if any rulegarden-domain sem is malformed or contradicts its bound
// vector (a forged semantics/vector pair). Entries without a sem are legacy — importable
// under the label rule, but noted so a reader knows they carry no verified semantics.
func verifyPackSems(pack *engine.Pack, stdout io.Writer) bool {
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
			fmt.Fprintf(stdout, "  sem %d:%d : present (domain %q) — not verifiable by this tool\n", pack.Site, e.Seq, probe.Domain)
			continue
		}
		if pack.VocabSeed != rulegarden.VocabSeed {
			fmt.Fprintf(stdout, "  sem %d:%d : rulegarden domain but foreign vocab seed %d — cannot re-encode\n", pack.Site, e.Seq, pack.VocabSeed)
			ok = false
			continue
		}
		ls, err := rulegarden.ParseLessonSem(e.Sem)
		if err != nil {
			fmt.Fprintf(stdout, "  sem %d:%d : INVALID (%v)\n", pack.Site, e.Seq, err)
			ok = false
			continue
		}
		actHV, err := vocab.ActionHV(ls.Action)
		if err != nil {
			fmt.Fprintf(stdout, "  sem %d:%d : INVALID (%v)\n", pack.Site, e.Seq, err)
			ok = false
			continue
		}
		if vocab.EncodePercept(ls.Percept).Bind(actHV) != e.Bound {
			fmt.Fprintf(stdout, "  sem %d:%d : FORGED — bound vector contradicts the signed semantics (%s → %s)\n", pack.Site, e.Seq, ls.Percept, ls.Action)
			ok = false
			continue
		}
		fmt.Fprintf(stdout, "  sem %d:%d : verified (%s → %s)\n", pack.Site, e.Seq, ls.Percept, ls.Action)
	}
	if legacy > 0 {
		fmt.Fprintf(stdout, "  sem       : %d legacy entr%s without machine-readable semantics (label-validated at import; cannot transfer)\n",
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
