package engine

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JGautam09/NeuroVSA/core"
)

// malformedHexVector returns a NumWords*8 hex string whose final word sets bit `bit`
// (0-63). Bits 16-63 are non-canonical and must be rejected.
func malformedHexVector(bit uint) string {
	raw := make([]byte, core.NumWords*8)
	binary.LittleEndian.PutUint64(raw[(core.NumWords-1)*8:], uint64(1)<<bit)
	return hex.EncodeToString(raw)
}

// TestPackRejectsNonCanonicalVector is the P0-2 regression: the reproduced panic case
// (final-word bit 16 → counter index 10000, one past [10000]uint32) must now be a clean
// error at JSON decode — never a panic.
func TestPackRejectsNonCanonicalVector(t *testing.T) {
	packJSON := `{"name":"evil","vocab_seed":"0","site":"1","entries":[{"seq":1,"label":"x","bound_hex":"` +
		malformedHexVector(16) + `"}]}`
	var p Pack
	err := json.Unmarshal([]byte(packJSON), &p)
	if err == nil {
		t.Fatal("expected non-canonical bound_hex to be rejected at unmarshal, got nil error")
	}
	if !strings.Contains(err.Error(), "non-canonical") {
		t.Fatalf("want non-canonical rejection, got %v", err)
	}
}

// TestCanonicalVectorAccepted: a valid vector (any of the low 16 bits set, high bits clear)
// still round-trips.
func TestCanonicalVectorAccepted(t *testing.T) {
	raw := make([]byte, core.NumWords*8)
	binary.LittleEndian.PutUint64(raw[(core.NumWords-1)*8:], 0x8001) // bits 0 and 15
	packJSON := `{"name":"ok","vocab_seed":"0","site":"1","entries":[{"seq":1,"label":"x","bound_hex":"` +
		hex.EncodeToString(raw) + `"}]}`
	var p Pack
	if err := json.Unmarshal([]byte(packJSON), &p); err != nil {
		t.Fatalf("canonical vector rejected: %v", err)
	}
	if _, err := p.Memory(); err != nil {
		t.Fatalf("canonical pack failed to materialize: %v", err)
	}
}

// TestEveryExcessBitRejected: each of the 48 excess bits must be caught individually.
func TestEveryExcessBitRejected(t *testing.T) {
	for bit := uint(16); bit < 64; bit++ {
		var hv core.Hypervector
		hv.Vector[core.NumWords-1] = uint64(1) << bit
		if err := hv.ValidateCanonical(); err == nil {
			t.Fatalf("excess bit %d was not rejected", bit)
		}
	}
}

// FuzzPackUnmarshal: no pack JSON — however malformed — may panic. If it decodes, Memory()
// must also not panic (it errors or succeeds). This is the standing guard against the whole
// class of "hostile vector crashes the runtime" bugs.
func FuzzPackUnmarshal(f *testing.F) {
	f.Add(`{"name":"n","vocab_seed":"0","site":"1","entries":[]}`)
	f.Add(`{"name":"n","vocab_seed":"0","site":"1","entries":[{"seq":1,"label":"l","bound_hex":"` + malformedHexVector(63) + `"}]}`)
	f.Add(`{"vocab_seed":"99999999999999999999","site":"1","entries":[]}`)
	f.Fuzz(func(t *testing.T, s string) {
		var p Pack
		if err := json.Unmarshal([]byte(s), &p); err != nil {
			return
		}
		_, _ = p.Memory() // must never panic
	})
}

// FuzzMemoryUnmarshalBinary: no byte string may panic UnmarshalBinary (the untrusted
// memory-image loader). It errors or loads.
func FuzzMemoryUnmarshalBinary(f *testing.F) {
	m := NewAssociativeMemory()
	m.SetVocabSeed(7)
	img, _ := m.MarshalBinary()
	f.Add(img)
	f.Add([]byte{})
	f.Add([]byte("NVSA"))
	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("UnmarshalBinary panicked on crafted image: %v", r)
			}
		}()
		nm := NewAssociativeMemory()
		_ = nm.UnmarshalBinary(data)
	})
}
