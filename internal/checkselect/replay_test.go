package checkselect

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

func TestJSONArtifactReadWriteSizeLimit(t *testing.T) {
	var out bytes.Buffer
	atLimit := strings.Repeat("a", maxArtifactBytes-3) // quoted string plus newline
	if err := WriteArtifact(&out, atLimit); err != nil || out.Len() != maxArtifactBytes {
		t.Fatalf("writing at limit: %d bytes, %v", out.Len(), err)
	}
	filename := filepath.Join(t.TempDir(), "artifact.json")
	if err := os.WriteFile(filename, out.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadJSON[string](filename); err != nil || got != atLimit {
		t.Fatalf("reading at limit: %d bytes, %v", len(got), err)
	}
	out.Reset()
	if err := WriteArtifact(&out, atLimit+"a"); err == nil || out.Len() != 0 {
		t.Fatalf("oversized artifact: wrote %d bytes, err %v", out.Len(), err)
	}
}

func TestPackUpdatesMergeAndReplay(t *testing.T) {
	stages, err := PackDemo()
	if err != nil {
		t.Fatal(err)
	}
	updated, conflict, revoked := stages["updated"], stages["conflict"], stages["revoked"]
	for name, want := range map[string]string{"initial": "api-focused", "updated": "integration-smoke", "conflict": "", "revoked": ""} {
		s := stages[name]
		r := s.Receipt.Recommendation
		if !slices.Equal(r.Required, []string{"unit-suite"}) ||
			strings.Join(r.Optional, ",") != want || (name == "conflict") != r.PackConflict {
			t.Errorf("%s: unexpected recommendation %+v", name, r)
		}
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		var loaded Replay
		if err := decodeJSON(data, &loaded); err != nil {
			t.Fatal(err)
		}
		if err := loaded.Verify(s.Receipt.SnapshotSHA256); err != nil {
			t.Errorf("%s: replay after JSON round trip: %v", name, err)
		}
	}
	// Different peers saw an old update, a conflicting revision, and a revocation.
	a := *updated.Snapshot.Bundle
	b := *conflict.Snapshot.Bundle
	forward, err := MergeBundles(*updated.Snapshot.Trust, a, b, *revoked.Snapshot.Bundle)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := MergeBundles(*updated.Snapshot.Trust, *revoked.Snapshot.Bundle, b, a, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(forward.Packs) != 4 || digestJSON(forward) != digestJSON(reverse) || digestJSON(forward) != digestJSON(*revoked.Snapshot.Bundle) {
		t.Fatal("peers disagree after merging update, conflict, replay, and revocation")
	}
	if err := updated.Verify(revoked.Receipt.SnapshotSHA256); err == nil {
		t.Fatal("verified a historical receipt with the digest of another snapshot")
	}
	altered := updated
	altered.Receipt.Recommendation.Required = []string{"api-focused"}
	if err := altered.Verify(""); err == nil {
		t.Fatal("verified a changed required-check output")
	}
	tampered := a
	tampered.Packs = slices.Clone(a.Packs)
	tampered.Packs[0].Payload.Revision++
	if _, err := MergeBundles(*updated.Snapshot.Trust, tampered); err == nil {
		t.Fatal("accepted a signed payload with a modified revision")
	}
}

func TestPacksCannotOverrideLocalScopeOrChecks(t *testing.T) {
	const scope = "example/project"
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	policy := Policy{Version: 1, Scope: scope, Checks: []string{"required", "allowed"}, Always: []string{"required"},
		Rules: []Rule{{ID: "api", Paths: []string{"api/**"}}}}
	trust := Trust{Version: 1, Scope: scope, Signers: []TrustedSigner{
		{PublicKey: key.Public().(ed25519.PublicKey), Checks: []string{"required", "allowed", "retired"}},
	}}
	changes := []Change{{Status: "M", Path: "api/server.go"}}
	payload := PackPayload{Version: 1, Scope: scope, Name: "example", Revision: core.QuotedU64(1),
		Examples: []PackExample{{ID: "one", Changes: changes, Optional: []string{"required", "allowed", "retired"}}}}
	event, err := SignPack(payload, key)
	if err != nil {
		t.Fatal(err)
	}
	bundle := Bundle{Version: 1, Packs: []SignedPack{event}}
	snapshot, err := PrepareSnapshot(Snapshot{Version: 1, Policy: policy, Trust: &trust, Bundle: &bundle})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := snapshot.Evaluate(changes, false)
	if err != nil {
		t.Fatal(err)
	}
	got := replay.Receipt.Recommendation
	if !slices.Equal(got.Required, []string{"required"}) || !slices.Equal(got.Optional, []string{"allowed"}) {
		t.Fatalf("pack bypassed the local catalog or downgraded a required check: %+v", got)
	}
	trust.Signers[0].Checks = []string{"allowed"}
	if _, err := MergeBundles(trust, bundle); err == nil {
		t.Fatal("accepted checks outside the signer allowlist")
	}
	trust.Scope = "another/project"
	if _, err := MergeBundles(trust, bundle); err == nil {
		t.Fatal("accepted a signed pack for another repository")
	}
}

func TestHDCAddsOnlyOptionalEvidence(t *testing.T) {
	policy := Policy{Version: 1, Checks: []string{"ci", "edge", "api"}, Always: []string{"ci"},
		Rules: []Rule{{ID: "api", Paths: []string{"api/**"}}, {ID: "edge", Paths: []string{"edge/**"}}}}
	api := []Change{{Status: "M", Path: "api/server.go"}}
	edge := []Change{{Status: "D", Path: "edge/handler.js"}}
	predict := hdcSelector(policy, []Case{
		{ID: "api-fixture", Split: "development", Changes: api, Expected: Expectations{Optional: []string{"api"}}},
		{ID: "edge-fixture", Split: "development", Changes: edge, Expected: Expectations{Optional: []string{"edge"}}},
	})
	got := predict(api)
	if !slices.Equal(got.Required, []string{"ci"}) || !slices.Equal(got.Optional, []string{"api"}) ||
		got.HDCProof == nil || got.HDCProof.Chosen != "check:api" {
		t.Fatalf("expected an optional, explained prediction: %+v", got)
	}
	unknown := predict([]Change{{Status: "A", Path: "unknown/foo.go"}})
	if unknown.Fallback == "" || unknown.HDCProof != nil || len(unknown.Optional) != 0 {
		t.Fatalf("HDC masked a fallback: %+v", unknown)
	}
}
