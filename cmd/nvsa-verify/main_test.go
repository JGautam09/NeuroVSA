package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/engine"
	"github.com/JGautam09/NeuroVSA/rulegarden"
)

// End-to-end tests for the trust-critical verifier CLI: deterministic fixtures (a taught
// world's receipt + brain image, signed and unsigned packs, a signed world pack) run
// through run() with exit codes and key output lines asserted — including the tamper
// paths, which are the reason the tool exists.

func testKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(200 - i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// taughtWorld builds the standard fixture world: one creature taught to flee a nearby
// predator, one predator beside it, stepped once so a decision (and its receipt snapshot)
// exists.
func taughtWorld(t *testing.T, seed uint64) *rulegarden.World {
	t.Helper()
	w := rulegarden.NewWorld(seed)
	for _, e := range []rulegarden.Event{
		{Op: "spawn_creature", X: 12, Y: 12},
		{Op: "spawn_object", Kind: rulegarden.KindPredator, X: 14, Y: 12},
		{Op: "teach", Creature: 1, Percept: &rulegarden.PerceptSpec{Sees: "predator", Dist: rulegarden.DistNear, Dir: "E"}, Action: rulegarden.ActMoveAway},
	} {
		if err := w.Apply(e); err != nil {
			t.Fatalf("apply %s: %v", e.Op, err)
		}
	}
	w.Step()
	return w
}

// runCLI invokes run() and returns (exit code, stdout, stderr).
func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCertVerifyAndTamper(t *testing.T) {
	dir := t.TempDir()
	w := taughtWorld(t, 71)
	cert, brain, err := w.CertifyCreature(1)
	if err != nil {
		t.Fatal(err)
	}
	cert.Sign(testKey())
	certJSON, err := json.Marshal(cert)
	if err != nil {
		t.Fatal(err)
	}
	certPath := writeFile(t, dir, "receipt.json", certJSON)
	memPath := writeFile(t, dir, "brain.bin", brain)

	code, out, _ := runCLI(t, "-cert", certPath, "-memory", memPath, "-require-signature")
	if code != 0 {
		t.Fatalf("valid signed receipt must verify, got exit %d:\n%s", code, out)
	}
	for _, want := range []string{"signature         : valid", "memory fingerprint: OK", "re-execution      : OK", "VERIFIED"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}

	// Tamper: certify a different executed action. Re-execution must fail, exit 1.
	var tampered engine.DecisionCertificate
	if err := json.Unmarshal(certJSON, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.ExecutedAction = "action:" + rulegarden.ActEat
	tamperedJSON, _ := json.Marshal(tampered)
	tamperedPath := writeFile(t, dir, "tampered.json", tamperedJSON)
	code, out, _ = runCLI(t, "-cert", tamperedPath, "-memory", memPath)
	if code != 1 {
		t.Fatalf("tampered receipt must fail, got exit %d:\n%s", code, out)
	}

	// Wrong memory: teach one more lesson and save — the fingerprint no longer matches the
	// decision-time image the receipt anchors to.
	if err := w.Apply(rulegarden.Event{Op: "teach", Creature: 1, Percept: &rulegarden.PerceptSpec{Sees: "food", Dist: rulegarden.DistNear, Dir: "N"}, Action: rulegarden.ActEat}); err != nil {
		t.Fatal(err)
	}
	drifted, err := w.Creatures[0].Brain.Memory().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	driftedPath := writeFile(t, dir, "drifted.bin", drifted)
	code, out, _ = runCLI(t, "-cert", certPath, "-memory", driftedPath)
	if code != 1 || !strings.Contains(out, "memory fingerprint: FAILED") {
		t.Fatalf("receipt against a drifted memory must fail its fingerprint, got exit %d:\n%s", code, out)
	}
}

func TestCertRequiresMemoryFlag(t *testing.T) {
	code, _, errOut := runCLI(t, "-cert", "whatever.json")
	if code != 1 || !strings.Contains(errOut, "-cert requires -memory") {
		t.Fatalf("want the -memory usage error, got exit %d stderr %q", code, errOut)
	}
}

func TestPackVerifySignedSemAndForgeries(t *testing.T) {
	dir := t.TempDir()
	w := taughtWorld(t, 72)
	packs, err := w.BrainPacks("cli-test", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) != 1 {
		t.Fatalf("want one pack, got %d", len(packs))
	}
	pack := packs[0]
	pack.Sign(testKey())

	packJSON, _ := json.Marshal(pack)
	packPath := writeFile(t, dir, "pack.json", packJSON)
	code, out, _ := runCLI(t, "-pack", packPath, "-require-signature")
	if code != 0 {
		t.Fatalf("signed sem pack must verify, got exit %d:\n%s", code, out)
	}
	for _, want := range []string{"signature: valid", ": verified (sees:predator,near,E → move-away)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}

	// Forged sem: claims food→eat over the flee vector. (Signature breaks too — assert the
	// sem check independently by leaving the pack unsigned.)
	forged := pack
	forged.Entries = append([]engine.PackEntry(nil), pack.Entries...)
	forged.Entries[0].Sem = rulegarden.EncodeLessonSem(rulegarden.PerceptSpec{Sees: "food", Dist: rulegarden.DistNear, Dir: "N"}, rulegarden.ActEat, "")
	forged.PublicKey, forged.Signature = nil, nil
	forgedJSON, _ := json.Marshal(forged)
	forgedPath := writeFile(t, dir, "forged.json", forgedJSON)
	code, out, _ = runCLI(t, "-pack", forgedPath)
	if code != 1 || !strings.Contains(out, "FORGED") {
		t.Fatalf("forged sem must fail with a FORGED verdict, got exit %d:\n%s", code, out)
	}

	// Legacy entry (no sem): reported as legacy, not a failure.
	legacy := pack
	legacy.Entries = append([]engine.PackEntry(nil), pack.Entries...)
	legacy.Entries[0].Sem = ""
	legacy.PublicKey, legacy.Signature = nil, nil
	legacyJSON, _ := json.Marshal(legacy)
	legacyPath := writeFile(t, dir, "legacy.json", legacyJSON)
	code, out, _ = runCLI(t, "-pack", legacyPath)
	if code != 0 || !strings.Contains(out, "1 legacy entry without machine-readable semantics") {
		t.Fatalf("legacy pack must pass with a legacy note, got exit %d:\n%s", code, out)
	}

	// Unsigned + -require-signature → exit 1.
	code, _, _ = runCLI(t, "-pack", legacyPath, "-require-signature")
	if code != 1 {
		t.Fatalf("unsigned pack with -require-signature must fail, got exit %d", code)
	}

	// Installation status against the exporting brain's image.
	img, err := w.Creatures[0].Brain.Memory().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	memPath := writeFile(t, dir, "brain.bin", img)
	code, out, _ = runCLI(t, "-pack", packPath, "-memory", memPath)
	if code != 0 || !strings.Contains(out, "in memory: 1 active, 0 revoked, 0 not installed") {
		t.Fatalf("installation status wrong, got exit %d:\n%s", code, out)
	}
}

func TestWorldVerifySignedAndTampered(t *testing.T) {
	dir := t.TempDir()
	w := taughtWorld(t, 73)
	p := w.Export()
	p.Sign(testKey())
	signedJSON, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	signedPath := writeFile(t, dir, "world.json", signedJSON)

	code, out, _ := runCLI(t, "-world", signedPath, "-require-signature")
	if code != 0 {
		t.Fatalf("signed world must verify, got exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "signature : valid") || !strings.Contains(out, "replay    : OK — world hash "+w.Hash()) {
		t.Fatalf("output missing signature/hash lines:\n%s", out)
	}

	// Tamper an event after signing: replay must refuse the pack.
	var tampered rulegarden.Pack
	if err := json.Unmarshal(signedJSON, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Events[len(tampered.Events)-1].Action = rulegarden.ActEat
	tamperedJSON, _ := json.Marshal(tampered)
	tamperedPath := writeFile(t, dir, "tampered.json", tamperedJSON)
	code, _, errOut := runCLI(t, "-world", tamperedPath)
	if code != 1 || !strings.Contains(errOut, "signature is invalid") {
		t.Fatalf("tampered signed world must be rejected, got exit %d stderr %q", code, errOut)
	}

	// Unsigned world: replays fine, but -require-signature fails it.
	unsigned := w.Export()
	unsignedJSON, _ := json.Marshal(unsigned)
	unsignedPath := writeFile(t, dir, "unsigned.json", unsignedJSON)
	if code, _, _ = runCLI(t, "-world", unsignedPath); code != 0 {
		t.Fatalf("unsigned world must replay, got exit %d", code)
	}
	code, out, _ = runCLI(t, "-world", unsignedPath, "-require-signature")
	if code != 1 || !strings.Contains(out, "FAILED: pack is unsigned") {
		t.Fatalf("unsigned world with -require-signature must fail, got exit %d:\n%s", code, out)
	}
}

func TestUsageAndBadInputs(t *testing.T) {
	if code, _, _ := runCLI(t); code != 2 {
		t.Fatal("no mode flags must exit 2 (usage)")
	}
	if code, _, _ := runCLI(t, "-not-a-flag"); code != 2 {
		t.Fatal("unknown flag must exit 2")
	}
	if code, _, errOut := runCLI(t, "-world", filepath.Join(t.TempDir(), "missing.json")); code != 1 || !strings.Contains(errOut, "read world pack") {
		t.Fatal("missing file must exit 1 with a read error")
	}
	dir := t.TempDir()
	garbage := writeFile(t, dir, "garbage.json", []byte("not json"))
	if code, _, _ := runCLI(t, "-world", garbage); code != 1 {
		t.Fatal("garbage world pack must exit 1")
	}
	if code, _, _ := runCLI(t, "-pack", garbage); code != 1 {
		t.Fatal("garbage lesson pack must exit 1")
	}
}
