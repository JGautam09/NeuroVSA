package rulegarden

import (
	"crypto/ed25519"
	"encoding/json"

	"github.com/JGautam09/NeuroVSA/engine"
)

// World-pack signing (Phase A of the trust arc): a shared world can carry its author's
// ed25519 signature, mirroring engine.Pack's scheme. The signature covers CanonicalBytes —
// the pack's replayable content (version, seed, ticks, events), excluding the signature
// fields themselves. Because merge_brains events embed foreign packs verbatim, a signed
// pack stays tamper-evident even when quoted inside another player's world: replay verifies
// every embedded signature it encounters.
//
// Signing is optional. An unsigned pack replays exactly as before ("unsigned, replay-
// verifiable only"); a pack whose signature does not match its content is refused outright.

// CanonicalBytes is the deterministic encoding signatures cover: the JSON of the pack's
// content fields only (PublicKey and Signature excluded). Pack and Event are fixed structs
// with no maps, so encoding/json marshals them byte-deterministically in field order —
// the same property the world Hash and golden replay tests already rely on.
func (p *Pack) CanonicalBytes() []byte {
	shadow := struct {
		Version int     `json:"version"`
		Seed    uint64  `json:"seed"`
		Ticks   int     `json:"ticks"`
		Events  []Event `json:"events"`
	}{p.Version, uint64(p.Seed), p.Ticks, p.Events}
	b, err := json.Marshal(shadow)
	if err != nil {
		return nil // plain-data structs cannot fail to marshal; guard defensively
	}
	return b
}

// Sign binds the pack to an ed25519 key. Call after the pack's content is final — any
// later mutation (including of an embedded foreign pack) invalidates the signature.
func (p *Pack) Sign(priv ed25519.PrivateKey) {
	p.PublicKey = append([]byte(nil), priv.Public().(ed25519.PublicKey)...)
	p.Signature = ed25519.Sign(priv, p.CanonicalBytes())
}

// VerifySignature checks the embedded signature over the canonical encoding. False for
// unsigned packs — callers distinguish "unsigned" (len(Signature)==0, allowed) from
// "invalid" (signed but failing, refused) themselves.
func (p *Pack) VerifySignature() bool {
	if len(p.PublicKey) != ed25519.PublicKeySize || len(p.Signature) == 0 {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(p.PublicKey), p.CanonicalBytes(), p.Signature)
}

// SignerFingerprint is the short display identity of the pack's author key ("" when
// unsigned). Trust decisions verify the signature against the full embedded key; the
// fingerprint is the human-readable handle shown in the UI and trusted-signers lists.
func (p *Pack) SignerFingerprint() string {
	return engine.KeyFingerprint(p.PublicKey)
}
