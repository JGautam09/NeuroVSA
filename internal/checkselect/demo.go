package checkselect

import (
	"crypto/ed25519"
	"crypto/sha256"

	"github.com/JGautam09/NeuroVSA/core"
)

// PackDemo is the fixed sample used by RuleGarden. It uses a public sample key
// and resets on every run, without using the player's world or signing key.
func PackDemo() (map[string]Replay, error) {
	seed := sha256.Sum256([]byte("NeuroVSA browser demonstration key"))
	key := ed25519.NewKeyFromSeed(seed[:])
	const scope = "example/project"
	policy := Policy{Version: 1, Scope: scope, Checks: []string{"unit-suite", "api-focused", "integration-smoke"},
		Always: []string{"unit-suite"}, Rules: []Rule{{ID: "api", Paths: []string{"api/**"}}}}
	trust := Trust{Version: 1, Scope: scope, Signers: []TrustedSigner{
		{PublicKey: key.Public().(ed25519.PublicKey), Checks: []string{"api-focused", "integration-smoke"}},
	}}
	changes := []Change{{Status: "M", Path: "api/routes.go"}}
	sign := func(revision uint64, check string) (SignedPack, error) {
		p := PackPayload{Version: 1, Scope: scope, Name: "api-checks", Revision: core.QuotedU64(revision), Revoked: check == ""}
		if check != "" {
			p.Examples = []PackExample{{ID: "routes-change", Changes: changes, Optional: []string{check}}}
		}
		return SignPack(p, key)
	}
	initial, err := sign(1, "api-focused")
	if err != nil {
		return nil, err
	}
	update, err := sign(2, "integration-smoke")
	if err != nil {
		return nil, err
	}
	conflict, err := sign(2, "api-focused")
	if err != nil {
		return nil, err
	}
	revoke, err := sign(3, "")
	if err != nil {
		return nil, err
	}
	stages := map[string][]SignedPack{
		"initial": {initial}, "updated": {initial, update},
		"conflict": {initial, update, conflict}, "revoked": {initial, update, conflict, revoke},
	}
	out := make(map[string]Replay, len(stages))
	for name, events := range stages {
		bundle := Bundle{Version: 1, Packs: events}
		snapshot, err := PrepareSnapshot(Snapshot{Version: 1, Policy: policy, Trust: &trust, Bundle: &bundle})
		if err != nil {
			return nil, err
		}
		replay, err := snapshot.Evaluate(changes, false)
		if err != nil {
			return nil, err
		}
		out[name] = replay
	}
	return out, nil
}
