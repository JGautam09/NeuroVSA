package core

import (
	"strconv"
)

// QuotedU64 is a uint64 that marshals to/from JSON as a DECIMAL STRING, not a JSON number.
//
// Why every uint64 that can cross JavaScript must use this: JavaScript has no 64-bit integer
// type — `JSON.parse` coerces numbers to IEEE-754 doubles, exact only up to 2^53. A site id,
// world seed, or sequence above that is silently corrupted by a single browser
// parse/stringify hop, which then breaks ed25519 signatures (computed over the true value)
// and deterministic replay (a corrupted seed produces a different world). Decimal strings
// survive every JSON implementation intact.
//
// Canonical *signing* bytes never use JSON — they encode the raw uint64 in binary — so this
// type only governs the JSON wire form, and the signed content is unaffected.
type QuotedU64 uint64

func (v QuotedU64) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatUint(uint64(v), 10) + `"`), nil
}

// UnmarshalJSON accepts a quoted decimal string (the canonical form) and also a bare JSON
// number, so packs written by older versions that emitted numbers still parse.
func (v *QuotedU64) UnmarshalJSON(b []byte) error {
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	u, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}
	*v = QuotedU64(u)
	return nil
}
