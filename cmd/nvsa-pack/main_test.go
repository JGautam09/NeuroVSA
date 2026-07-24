package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/rulegarden"
)

// worldPackJSON authors a small taught world and returns its (unsigned) pack JSON.
func worldPackJSON(t *testing.T, seed uint64) []byte {
	t.Helper()
	w := rulegarden.NewWorld(seed)
	for _, e := range []rulegarden.Event{
		{Op: "spawn_creature", X: 12, Y: 12},
		{Op: "spawn_object", Kind: rulegarden.KindPredator, X: 15, Y: 12},
		{Op: "teach", Creature: 1, Percept: &rulegarden.PerceptSpec{Sees: "predator", Dist: rulegarden.DistNear, Dir: "E"}, Action: rulegarden.ActMoveAway},
	} {
		if err := w.Apply(e); err != nil {
			t.Fatalf("apply %s: %v", e.Op, err)
		}
	}
	raw, err := w.ExportJSON()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func testSeedKey(t *testing.T, dir string) (string, ed25519.PrivateKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(100 + i)
	}
	kf := keyFile{RuleGardenKey: 1, SeedB64: base64.StdEncoding.EncodeToString(seed)}
	blob, _ := json.Marshal(kf)
	path := filepath.Join(dir, "key.json")
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, ed25519.NewKeyFromSeed(seed)
}

func TestPublishAndVerifyRegistry(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	keyPath, priv := testSeedKey(t, dir)

	packPath := filepath.Join(dir, "world.json")
	if err := os.WriteFile(packPath, worldPackJSON(t, 31), 0o644); err != nil {
		t.Fatal(err)
	}

	cmdPublish([]string{"-key", keyPath, "-in", packPath, "-name", "flee-demo", "-desc", "teaches fleeing", "-registry", reg})

	// The manifest exists, carries one world entry, and VerifyRegistry passes.
	if err := VerifyRegistry(reg, os.Stdout); err != nil {
		t.Fatalf("fresh registry must verify: %v", err)
	}
	var m manifest
	raw, _ := os.ReadFile(filepath.Join(reg, "index.json"))
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Packs) != 1 || m.Packs[0].Kind != "world" || m.Packs[0].Name != "flee-demo" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	wantPub := base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
	if m.Packs[0].AuthorPublicKey != wantPub {
		t.Fatal("manifest author key != publishing key")
	}

	// Republish under the same name -> still one entry (upsert, not append).
	cmdPublish([]string{"-key", keyPath, "-in", packPath, "-name", "flee-demo", "-desc", "v2", "-registry", reg})
	raw, _ = os.ReadFile(filepath.Join(reg, "index.json"))
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Packs) != 1 || m.Packs[0].Description != "v2" {
		t.Fatalf("republish must upsert, got %+v", m.Packs)
	}
}

func TestVerifyCatchesTampering(t *testing.T) {
	dir := t.TempDir()
	reg := filepath.Join(dir, "registry")
	keyPath, _ := testSeedKey(t, dir)
	packPath := filepath.Join(dir, "world.json")
	if err := os.WriteFile(packPath, worldPackJSON(t, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdPublish([]string{"-key", keyPath, "-in", packPath, "-name", "victim", "-desc", "d", "-registry", reg})

	packFile := filepath.Join(reg, "packs", "victim.json")
	pristine, _ := os.ReadFile(packFile)

	// 1a. Size-preserving byte flip in the pack file -> caught by sha256.
	flipped := strings.Replace(string(pristine), "move-away", "wove-away", 1)
	if flipped == string(pristine) {
		t.Fatal("tamper did not apply")
	}
	if err := os.WriteFile(packFile, []byte(flipped), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRegistry(reg, os.Stdout); err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want sha256 mismatch, got %v", err)
	}

	// 1b. Size-changing edit -> caught by the size gate even before hashing.
	if err := os.WriteFile(packFile, []byte(strings.Replace(string(pristine), "move-away", "eat", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRegistry(reg, os.Stdout); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("want size mismatch, got %v", err)
	}
	os.WriteFile(packFile, pristine, 0o644)

	// 2. Manifest author swapped -> embedded-key mismatch. Keep file/sha intact.
	idxFile := filepath.Join(reg, "index.json")
	var m manifest
	raw, _ := os.ReadFile(idxFile)
	json.Unmarshal(raw, &m)
	m.Packs[0].AuthorPublicKey = base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
	blob, _ := json.MarshalIndent(m, "", "  ")
	os.WriteFile(idxFile, append(blob, '\n'), 0o644)
	if err := VerifyRegistry(reg, os.Stdout); err == nil || !strings.Contains(err.Error(), "does not match manifest author") {
		t.Fatalf("want author mismatch, got %v", err)
	}
}

func TestSignRejectsUnreplayableWorld(t *testing.T) {
	dir := t.TempDir()
	_, priv := testSeedKey(t, dir)
	// A pack whose event log is inconsistent (teaches a creature that never spawned) must
	// not be signable — never sign what does not validate.
	bad := rulegarden.Pack{Version: 1, Seed: 5, Ticks: 0, Events: []rulegarden.Event{
		{Op: "teach", Creature: 9, Percept: &rulegarden.PerceptSpec{Sees: "food", Dist: rulegarden.DistNear, Dir: "N"}, Action: rulegarden.ActEat},
	}}
	raw, _ := json.Marshal(bad)
	if _, _, err := signRaw(raw, priv); err == nil {
		t.Fatal("signRaw must refuse a world pack that does not replay")
	}
}

// TestCommittedRegistryVerifies lints the registry that ships in this repository — the
// public reference registry must always pass its own linter. CI runs this on every push.
func TestCommittedRegistryVerifies(t *testing.T) {
	reg := filepath.Join("..", "..", "registry")
	if _, err := os.Stat(filepath.Join(reg, "index.json")); err != nil {
		t.Skipf("no committed registry at %s (yet)", reg)
	}
	if err := VerifyRegistry(reg, os.Stdout); err != nil {
		t.Fatalf("committed registry fails verification: %v", err)
	}
}
