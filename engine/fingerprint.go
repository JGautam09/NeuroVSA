package engine

import (
	"crypto/sha256"
	"encoding/hex"
)

// KeyFingerprint is the short human-readable identity of an ed25519 public key: the first
// 8 bytes of its SHA-256, hex-encoded (16 chars). It is a display/trust-list handle — the
// full public key embedded in certificates and packs remains the cryptographic identity;
// signatures are always verified against that, never against the fingerprint.
func KeyFingerprint(pub []byte) string {
	if len(pub) == 0 {
		return ""
	}
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}
