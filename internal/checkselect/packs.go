package checkselect

import (
	"bytes"
	"cmp"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"regexp"
	"slices"

	"github.com/JGautam09/NeuroVSA/core"
	"github.com/JGautam09/NeuroVSA/engine"
)

var scopeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}/[a-zA-Z0-9][a-zA-Z0-9_.-]{0,79}$`)

const (
	packDomain = "neurovsa-check-pack-v1\x00"
	maxEvents  = 256
)

type PackExample struct {
	ID       string   `json:"id"`
	Changes  []Change `json:"changes"`
	Optional []string `json:"optional"`
}

type PackPayload struct {
	Version  int            `json:"version"`
	Scope    string         `json:"scope"`
	Name     string         `json:"name"`
	Revision core.QuotedU64 `json:"revision"`
	Revoked  bool           `json:"revoked,omitempty"`
	Examples []PackExample  `json:"examples,omitempty"`
}

type SignedPack struct {
	Payload   PackPayload `json:"payload"`
	PublicKey []byte      `json:"public_key"`
	Signature []byte      `json:"signature"`
}

type TrustedSigner struct {
	PublicKey []byte   `json:"public_key"`
	Checks    []string `json:"checks"`
}

type Trust struct {
	Version int             `json:"version"`
	Scope   string          `json:"scope"`
	Signers []TrustedSigner `json:"signers"`
}

type Bundle struct {
	Version int          `json:"version"`
	Packs   []SignedPack `json:"packs"`
}

type PackMatch struct {
	Signer   string         `json:"signer"`
	Name     string         `json:"pack"`
	Revision core.QuotedU64 `json:"revision"`
	Example  string         `json:"example"`
	Checks   []string       `json:"checks"`
}

func (p PackPayload) validate() error {
	if p.Version != 1 || !scopeName.MatchString(p.Scope) || !identifier.MatchString(p.Name) || p.Revision == 0 {
		return fmt.Errorf("pack needs version 1, an owner/repo scope, a name, and a positive revision")
	}
	if len(p.Examples) > 64 || p.Revoked != (len(p.Examples) == 0) {
		return fmt.Errorf("provide 1–64 examples for an update, or a revocation with no examples")
	}
	seen := make(map[string]bool, len(p.Examples))
	for _, e := range p.Examples {
		if !identifier.MatchString(e.ID) || seen[e.ID] || len(e.Changes) == 0 || len(e.Changes) > 64 ||
			len(e.Optional) == 0 || len(e.Optional) > 32 || len(sortedUnique(e.Optional)) != len(e.Optional) {
			return fmt.Errorf("invalid or duplicate example %q", e.ID)
		}
		seen[e.ID] = true
		if _, err := Normalize(e.Changes); err != nil {
			return err
		}
		for _, id := range e.Optional {
			if !identifier.MatchString(id) {
				return fmt.Errorf("invalid check ID %q", id)
			}
		}
	}
	return nil
}

func (p PackPayload) signedBytes() []byte {
	data := []byte(packDomain)
	writeInt := func(n uint64) { data = binary.LittleEndian.AppendUint64(data, n) }
	writeString := func(s string) {
		writeInt(uint64(len(s)))
		data = append(data, s...)
	}
	writeInt(uint64(p.Version))
	writeString(p.Scope)
	writeString(p.Name)
	writeInt(uint64(p.Revision))
	if p.Revoked {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	writeInt(uint64(len(p.Examples)))
	for _, e := range p.Examples {
		writeString(e.ID)
		writeInt(uint64(len(e.Changes)))
		for _, c := range e.Changes {
			writeString(c.Status)
			writeString(c.Path)
			writeString(c.OldPath)
		}
		writeInt(uint64(len(e.Optional)))
		for _, id := range e.Optional {
			writeString(id)
		}
	}
	return data
}

func SignPack(payload PackPayload, key ed25519.PrivateKey) (SignedPack, error) {
	if err := payload.validate(); err != nil {
		return SignedPack{}, err
	}
	if len(key) != ed25519.PrivateKeySize {
		return SignedPack{}, fmt.Errorf("invalid pack signing key")
	}
	return SignedPack{payload, key.Public().(ed25519.PublicKey), ed25519.Sign(key, payload.signedBytes())}, nil
}

func (t Trust) validate() (map[string][]string, error) {
	if t.Version != 1 || !scopeName.MatchString(t.Scope) || len(t.Signers) > 32 {
		return nil, fmt.Errorf("trust needs version 1, an owner/repo scope, and at most 32 signers")
	}
	keys := make(map[string][]string, len(t.Signers))
	for _, s := range t.Signers {
		key := string(s.PublicKey)
		if _, duplicate := keys[key]; len(s.PublicKey) != ed25519.PublicKeySize || duplicate || len(s.Checks) > 256 ||
			len(sortedUnique(s.Checks)) != len(s.Checks) {
			return nil, fmt.Errorf("invalid or duplicate trusted signer")
		}
		for _, id := range s.Checks {
			if !identifier.MatchString(id) {
				return nil, fmt.Errorf("invalid permitted check ID %q", id)
			}
		}
		keys[key] = s.Checks
	}
	return keys, nil
}

type storedPack struct {
	SignedPack
	digest [32]byte
}

// MergeBundles joins authenticated events by exact signed payload. Identical
// events deduplicate, and ordering has no bearing on the winner or revocation.
func MergeBundles(trust Trust, inputs ...Bundle) (Bundle, error) {
	keys, err := trust.validate()
	if err != nil {
		return Bundle{}, err
	}
	events := make(map[[32]byte]storedPack)
	for _, bundle := range inputs {
		if bundle.Version != 1 || len(bundle.Packs) > maxEvents {
			return Bundle{}, fmt.Errorf("invalid bundle version or too many pack events")
		}
		for _, p := range bundle.Packs {
			allowed, trusted := keys[string(p.PublicKey)]
			if !trusted || p.Payload.Scope != trust.Scope {
				return Bundle{}, fmt.Errorf("pack %q uses an untrusted signer or a different scope", p.Payload.Name)
			}
			if err := p.Payload.validate(); err != nil {
				return Bundle{}, err
			}
			body := p.Payload.signedBytes()
			if !ed25519.Verify(p.PublicKey, body, p.Signature) {
				return Bundle{}, fmt.Errorf("invalid signature for pack %q", p.Payload.Name)
			}
			for _, e := range p.Payload.Examples {
				if len(Difference(e.Optional, allowed)) != 0 {
					return Bundle{}, fmt.Errorf("pack %q requests a check outside the signer's allowlist", p.Payload.Name)
				}
			}
			h := sha256.New()
			h.Write(p.PublicKey)
			h.Write(body)
			var digest [32]byte
			copy(digest[:], h.Sum(nil))
			events[digest] = storedPack{p, digest}
			if len(events) > maxEvents {
				return Bundle{}, fmt.Errorf("merged bundle exceeds %d pack events", maxEvents)
			}
		}
	}
	ordered := make([]storedPack, 0, len(events))
	for _, e := range events {
		ordered = append(ordered, e)
	}
	slices.SortFunc(ordered, func(a, b storedPack) int {
		return cmp.Or(bytes.Compare(a.PublicKey, b.PublicKey), cmp.Compare(a.Payload.Name, b.Payload.Name),
			cmp.Compare(a.Payload.Revision, b.Payload.Revision), bytes.Compare(a.digest[:], b.digest[:]))
	})
	result := Bundle{Version: 1, Packs: make([]SignedPack, 0, len(ordered))}
	for _, e := range ordered {
		result.Packs = append(result.Packs, e.SignedPack)
	}
	return result, nil
}

// augmentWithPacks only suggests checks on exact, normalized change matches.
// A revocation retires a signing key's pack name permanently. Multiple payloads
// for the same latest revision are ambiguous. A higher revision can settle that
// ambiguity, while conflicting suggestions from distinct packs are withheld.
func augmentWithPacks(r Result, p Policy, b Bundle, changes []Change) Result {
	if r.Fallback != "" {
		return r
	}
	var proposal []string
	for i := 0; i < len(b.Packs); {
		first := b.Packs[i]
		j, revoked := i, false
		for j < len(b.Packs) && bytes.Equal(b.Packs[j].PublicKey, first.PublicKey) && b.Packs[j].Payload.Name == first.Payload.Name {
			revoked = revoked || b.Packs[j].Payload.Revoked
			j++
		}
		last := b.Packs[j-1]
		if !revoked {
			latest := j - 1
			for latest > i && b.Packs[latest-1].Payload.Revision == last.Payload.Revision {
				latest--
			}
			for _, candidate := range b.Packs[latest:j] {
				for _, e := range candidate.Payload.Examples {
					normalized, _ := Normalize(e.Changes)
					if !slices.Equal(changes, normalized) {
						continue
					}
					// Local policy controls the current catalog. A pack cannot
					// reintroduce a retired check or downgrade a requirement.
					checks := []string{}
					for _, id := range sortedUnique(e.Optional) {
						if slices.Contains(p.Checks, id) && !slices.Contains(r.Required, id) {
							checks = append(checks, id)
						}
					}
					r.PackMatches = append(r.PackMatches, PackMatch{engine.KeyFingerprint(candidate.PublicKey),
						candidate.Payload.Name, candidate.Payload.Revision, e.ID, checks})
					if j-latest > 1 || (proposal != nil && !slices.Equal(proposal, checks)) {
						r.PackConflict = true
					}
					proposal = checks
				}
			}
		}
		i = j
	}
	if !r.PackConflict {
		r.Optional = sortedUnique(append(r.Optional, proposal...))
	}
	return r
}
