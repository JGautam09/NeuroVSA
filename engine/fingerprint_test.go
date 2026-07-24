package engine

import (
	"crypto/ed25519"
	"testing"
)

func TestKeyFingerprint(t *testing.T) {
	if got := KeyFingerprint(nil); got != "" {
		t.Fatalf("nil key fingerprint = %q, want empty", got)
	}

	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)

	got := KeyFingerprint(pub)
	if len(got) != 16 {
		t.Fatalf("fingerprint length = %d, want 16 hex chars", len(got))
	}
	// Golden: fixed key -> fixed fingerprint, the display identity shown in UIs and
	// trusted-signers lists. Must be stable across platforms and releases.
	const want = "56475aa75463474c"
	if got != want {
		t.Fatalf("fingerprint drift: got %s, want %s", got, want)
	}
	if again := KeyFingerprint(pub); again != got {
		t.Fatal("fingerprint must be deterministic")
	}
}
