package engine

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/JGautam09/NeuroVSA/core"
)

// Version is embedded in every issued certificate's SIGNED canonical bytes as provenance,
// so it must track the release (a stale value attests a false engine version). Bump it with
// each release. Verification of an EXISTING certificate uses that certificate's own recorded
// EngineVersion field, so bumping this never invalidates receipts issued by older engines.
const Version = "0.8.1"

// DecisionCertificate is a machine-checkable receipt for one cleanup decision: it records
// the exact inputs (state vector, candidate vocabulary, memory fingerprint) and outputs
// (ranked table, chosen token, contributing associations) of the computation that produced
// an action. Because the engine is integer-exact and the vocabulary is seed-derived, a
// verifier holding the referenced memory can RE-EXECUTE the decision and demand bit-exact
// agreement — the certificate is the literal computation, not a post-hoc explanation.
//
// Signatures are optional: an unsigned certificate is still replay-verifiable; a signed one
// additionally binds the receipt to a key (crypto/ed25519, over CanonicalBytes — a
// deterministic binary encoding, never JSON).
type DecisionCertificate struct {
	EngineVersion     string           `json:"engine_version"`
	VocabSeed         uint64           `json:"-"` // quoted via certJSON (core.QuotedU64)
	MemoryFingerprint string           `json:"memory_fingerprint"`
	State             core.Hypervector `json:"-"`                // hex in JSON (see marshalers)
	CandidateTokens   []string         `json:"candidate_tokens"` // ranking input order (tie-break order)
	Chosen            string           `json:"chosen"`
	Distance          int              `json:"distance"`
	Candidates        []core.Candidate `json:"candidates"`
	Contributors      []Contributor    `json:"contributors,omitempty"`
	// Policy fields make the ACTED-ON outcome re-derivable rather than merely asserted. When
	// Basis is set, the certificate declares the action the caller actually executed and the
	// margin policy under which it was chosen; VerifyAgainst re-derives both from the ranking
	// (see DerivePolicyOutcome) and requires an exact match. This closes the gap where a raw
	// cleanup winner (Chosen) differs from the executed action (e.g. an instinct override to
	// a default action).
	ExecutedAction string `json:"executed_action,omitempty"`
	Basis          string `json:"basis,omitempty"` // "lesson" | "generalization" | "instinct"
	MinMargin      int    `json:"min_margin,omitempty"`
	InstinctAction string `json:"instinct_action,omitempty"`
	// GeneralizationRadius is the Hamming radius within which Contributors were collected.
	// 0 means exact-only (the router's mode); a positive value means the certificate names
	// the nearest analogical sources (with their per-contributor Distance), so a verifier
	// re-derives a "generalization" basis from an identifiable source rather than trusting it.
	GeneralizationRadius int    `json:"generalization_radius,omitempty"`
	IssuedUnix           int64  `json:"issued_unix,omitempty"` // caller-supplied metadata
	Note                 string `json:"note,omitempty"`
	PublicKey            []byte `json:"public_key,omitempty"`
	Signature            []byte `json:"signature,omitempty"`
}

// DerivePolicyOutcome maps a ranked cleanup (plus how many of the chosen action's exact
// contributors exist) to an executed action and basis, under the margin-threshold policy
// RuleGarden-style callers use. It is the single rule applied at BOTH issue and verify, so a
// certificate's executed action is reproducible, not just attested:
//
//   - total 0 or winner margin < minMargin  → (instinctAction, "instinct")
//   - an exact contributor exists           → (winner, "lesson")
//   - otherwise                             → (winner, "generalization")
func DerivePolicyOutcome(total uint64, cands []core.Candidate, contributors []Contributor, minMargin int, instinctAction string) (action, basis string) {
	if len(cands) == 0 {
		return instinctAction, "instinct"
	}
	margin := 0
	if len(cands) >= 2 {
		margin = cands[1].Distance - cands[0].Distance
	}
	if total == 0 || margin < minMargin {
		return instinctAction, "instinct"
	}
	exact, near := 0, 0
	for _, ct := range contributors {
		if ct.Distance == 0 {
			exact++
		} else {
			near++
		}
	}
	if exact > 0 {
		return cands[0].Token, "lesson"
	}
	if near > 0 {
		return cands[0].Token, "generalization"
	}
	// Decisive margin but NO identifiable source (exact or near): interference, not
	// generalization. Report instinct rather than claim an analogy we cannot name.
	return instinctAction, "instinct"
}

// VerifyResult reports the three independent checks a certificate can pass.
type VerifyResult struct {
	Signed         bool   `json:"signed"`
	SignatureValid bool   `json:"signature_valid"` // false when unsigned
	FingerprintOK  bool   `json:"fingerprint_ok"`
	DecisionOK     bool   `json:"decision_ok"`
	Detail         string `json:"detail,omitempty"`
}

// OK reports whether every applicable check passed (signature only when present).
func (r VerifyResult) OK() bool {
	if r.Signed && !r.SignatureValid {
		return false
	}
	return r.FingerprintOK && r.DecisionOK
}

// RankCandidates ranks tokens by Hamming distance from query, ties broken by input order —
// the single tie-break rule every decision path in the engine uses.
func RankCandidates(query core.Hypervector, tokens []string, hvs []core.Hypervector) []core.Candidate {
	type ranked struct {
		core.Candidate
		idx int
	}
	all := make([]ranked, len(tokens))
	for i := range tokens {
		all[i] = ranked{core.Candidate{Token: tokens[i], Distance: core.HammingDistance(query, hvs[i])}, i}
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].Distance != all[b].Distance {
			return all[a].Distance < all[b].Distance
		}
		return all[a].idx < all[b].idx
	})
	out := make([]core.Candidate, len(all))
	for i := range all {
		out[i] = all[i].Candidate
	}
	return out
}

// IssueDecision executes one cleanup decision over the memory and returns it as a
// certificate. Candidate vectors are derived from the memory's vocab seed (never carried),
// so any verifier can re-derive them; the memory's convergent state is anchored by its
// Fingerprint. Requires a seeded vocabulary (the engine default).
func IssueDecision(mem *AssociativeMemory, state core.Hypervector, candidateTokens []string, contributorRadius int) (DecisionCertificate, error) {
	if len(candidateTokens) == 0 {
		return DecisionCertificate{}, fmt.Errorf("no candidate tokens")
	}
	dict := core.NewSeededTokenDictionary(mem.VocabSeed())
	hvs := make([]core.Hypervector, len(candidateTokens))
	for i, tok := range candidateTokens {
		hvs[i] = dict.GetOrRegister(tok)
	}

	query := mem.Matrix().Bind(state)
	cands := RankCandidates(query, candidateTokens, hvs)
	chosen := cands[0]

	var chosenHV core.Hypervector
	for i, tok := range candidateTokens {
		if tok == chosen.Token {
			chosenHV = hvs[i]
			break
		}
	}

	return DecisionCertificate{
		EngineVersion:        Version,
		VocabSeed:            mem.VocabSeed(),
		MemoryFingerprint:    mem.Fingerprint(),
		State:                state,
		CandidateTokens:      append([]string(nil), candidateTokens...),
		Chosen:               chosen.Token,
		Distance:             chosen.Distance,
		Candidates:           cands,
		Contributors:         mem.Contributors(state.Bind(chosenHV), contributorRadius),
		GeneralizationRadius: contributorRadius,
	}, nil
}

// VerifyAgainst re-executes the certified decision against a memory and checks all claims:
// the memory's fingerprint must match, and re-ranking the candidate vocabulary against the
// recomputed query must reproduce the chosen token, its distance, the full table, and the
// contributor set bit-for-bit.
func (c *DecisionCertificate) VerifyAgainst(mem *AssociativeMemory) VerifyResult {
	res := VerifyResult{Signed: len(c.Signature) > 0}
	if res.Signed {
		res.SignatureValid = c.VerifySignature()
		if !res.SignatureValid {
			res.Detail = "signature does not match certificate contents"
		}
	}

	res.FingerprintOK = mem.Fingerprint() == c.MemoryFingerprint
	if !res.FingerprintOK {
		res.Detail = appendDetail(res.Detail, "memory fingerprint differs from the certified state")
	}

	reissued, err := IssueDecision(mem, c.State, c.CandidateTokens, c.GeneralizationRadius)
	if err != nil {
		res.Detail = appendDetail(res.Detail, "re-execution failed: "+err.Error())
		return res
	}
	res.DecisionOK = reissued.Chosen == c.Chosen &&
		reissued.Distance == c.Distance &&
		candidatesEqual(reissued.Candidates, c.Candidates) &&
		contributorsEqual(reissued.Contributors, c.Contributors)
	if !res.DecisionOK {
		res.Detail = appendDetail(res.Detail, "re-execution does not reproduce the certified decision")
	}

	// When the certificate declares an executed action under a margin policy, re-derive it
	// from the re-executed ranking and require an exact match — so the receipt certifies the
	// action actually taken, not just the raw cleanup winner.
	if c.Basis != "" {
		action, basis := DerivePolicyOutcome(mem.Total(), reissued.Candidates, reissued.Contributors, c.MinMargin, c.InstinctAction)
		if action != c.ExecutedAction || basis != c.Basis {
			res.DecisionOK = false
			res.Detail = appendDetail(res.Detail, "re-execution does not reproduce the certified executed action/basis")
		}
	}
	return res
}

// Sign binds the certificate to an ed25519 key. Call after all content fields are final.
func (c *DecisionCertificate) Sign(priv ed25519.PrivateKey) {
	c.PublicKey = append([]byte(nil), priv.Public().(ed25519.PublicKey)...)
	c.Signature = ed25519.Sign(priv, c.CanonicalBytes())
}

// VerifySignature checks the embedded signature over the canonical encoding.
func (c *DecisionCertificate) VerifySignature() bool {
	if len(c.PublicKey) != ed25519.PublicKeySize || len(c.Signature) == 0 {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(c.PublicKey), c.CanonicalBytes(), c.Signature)
}

// CanonicalBytes is the deterministic binary encoding signatures cover — every content
// field in fixed order with length prefixes, never JSON (no canonicalization pitfalls).
// PublicKey and Signature are excluded.
func (c *DecisionCertificate) CanonicalBytes() []byte {
	var b bytes.Buffer
	b.WriteString("NVSA-CERT1")
	writeStr := func(s string) {
		var l [4]byte
		binary.LittleEndian.PutUint32(l[:], uint32(len(s)))
		b.Write(l[:])
		b.WriteString(s)
	}
	writeU64 := func(v uint64) {
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], v)
		b.Write(buf[:])
	}

	writeStr(c.EngineVersion)
	writeU64(c.VocabSeed)
	writeStr(c.MemoryFingerprint)
	for i := 0; i < core.NumWords; i++ {
		writeU64(c.State.Vector[i])
	}
	writeU64(uint64(len(c.CandidateTokens)))
	for _, t := range c.CandidateTokens {
		writeStr(t)
	}
	writeStr(c.Chosen)
	writeU64(uint64(int64(c.Distance)))
	writeU64(uint64(len(c.Candidates)))
	for _, cand := range c.Candidates {
		writeStr(cand.Token)
		writeU64(uint64(int64(cand.Distance)))
	}
	writeU64(uint64(len(c.Contributors)))
	for _, ct := range c.Contributors {
		writeU64(ct.ID.Site)
		writeU64(ct.ID.Seq)
		writeStr(ct.Label)
		writeU64(uint64(int64(ct.Distance)))
	}
	writeStr(c.ExecutedAction)
	writeStr(c.Basis)
	writeU64(uint64(int64(c.MinMargin)))
	writeStr(c.InstinctAction)
	writeU64(uint64(int64(c.GeneralizationRadius)))
	writeU64(uint64(c.IssuedUnix))
	writeStr(c.Note)
	return b.Bytes()
}

// ---- JSON (state vector as hex, so receipts survive JavaScript's number precision) ----

type certJSON struct {
	StateHex string `json:"state_hex"`
	// VocabSeed shadows the aliased uint64 field so it crosses JavaScript as a quoted
	// string (see core.QuotedU64); the aliased field is json:"-".
	VocabSeed core.QuotedU64 `json:"vocab_seed"`
	*certAlias
}

type certAlias DecisionCertificate

func (c DecisionCertificate) MarshalJSON() ([]byte, error) {
	a := certAlias(c)
	return json.Marshal(certJSON{StateHex: hvToHex(c.State), VocabSeed: core.QuotedU64(c.VocabSeed), certAlias: &a})
}

func (c *DecisionCertificate) UnmarshalJSON(data []byte) error {
	var j certJSON
	j.certAlias = (*certAlias)(c)
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	hv, err := hvFromHex(j.StateHex)
	if err != nil {
		return fmt.Errorf("state_hex: %w", err)
	}
	c.State = hv
	c.VocabSeed = uint64(j.VocabSeed)
	return nil
}

// ---- helpers ----

func hvToHex(hv core.Hypervector) string {
	out := make([]byte, 0, core.NumWords*16)
	for i := 0; i < core.NumWords; i++ {
		var w [8]byte
		binary.LittleEndian.PutUint64(w[:], hv.Vector[i])
		out = append(out, w[:]...)
	}
	return hex.EncodeToString(out)
}

func hvFromHex(s string) (core.Hypervector, error) {
	var hv core.Hypervector
	raw, err := hex.DecodeString(s)
	if err != nil {
		return hv, err
	}
	if len(raw) != core.NumWords*8 {
		return hv, fmt.Errorf("hex vector has %d bytes, want %d", len(raw), core.NumWords*8)
	}
	for i := 0; i < core.NumWords; i++ {
		hv.Vector[i] = binary.LittleEndian.Uint64(raw[i*8:])
	}
	// Reject non-canonical vectors at the untrusted decode boundary — an excess bit would
	// otherwise index the vote-counter array out of range downstream (see
	// core.ValidateCanonical). This guards every JSON path: pack entries, certificate state.
	if err := hv.ValidateCanonical(); err != nil {
		return core.Hypervector{}, err
	}
	return hv, nil
}

func candidatesEqual(a, b []core.Candidate) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contributorsEqual(a, b []Contributor) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func appendDetail(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}
