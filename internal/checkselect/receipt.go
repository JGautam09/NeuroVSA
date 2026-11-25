package checkselect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
)

func digestJSON(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Snapshot is the complete local selection policy at recommendation time.
type Snapshot struct {
	Version int     `json:"version"`
	Policy  Policy  `json:"policy"`
	Trust   *Trust  `json:"trust,omitempty"`
	Bundle  *Bundle `json:"bundle,omitempty"`
}

type Receipt struct {
	Version        int      `json:"version"`
	SnapshotSHA256 string   `json:"snapshot_sha256"`
	Changes        []Change `json:"changes"`
	MissingHistory bool     `json:"missing_history,omitempty"`
	Recommendation Result   `json:"recommendation"`
}

type Replay struct {
	Receipt  Receipt  `json:"receipt"`
	Snapshot Snapshot `json:"snapshot"`
}

// PrepareSnapshot validates signatures and puts bundle events in merge order
// once. A long-lived client can evaluate many changes over the same snapshot.
func PrepareSnapshot(s Snapshot) (Snapshot, error) {
	if s.Version != 1 {
		return Snapshot{}, fmt.Errorf("unsupported snapshot version %d", s.Version)
	}
	if err := s.Policy.validate(); err != nil {
		return Snapshot{}, err
	}
	if (s.Trust == nil) != (s.Bundle == nil) {
		return Snapshot{}, fmt.Errorf("pack recommendations require both a trust file and a bundle")
	}
	if s.Trust != nil {
		if s.Policy.Scope != s.Trust.Scope {
			return Snapshot{}, fmt.Errorf("policy scope and trust scope differ")
		}
		bundle, err := MergeBundles(*s.Trust, *s.Bundle)
		if err != nil {
			return Snapshot{}, err
		}
		s.Bundle = &bundle
	}
	return s, nil
}

func (s Snapshot) Evaluate(changes []Change, missingHistory bool) (Replay, error) {
	normalized, err := Normalize(changes)
	if err != nil {
		return Replay{}, err
	}
	if missingHistory && len(normalized) > 0 {
		return Replay{}, fmt.Errorf("a missing-history result cannot include changes")
	}
	result := s.Policy.MissingHistory()
	if !missingHistory {
		result = s.Policy.Recommend(normalized)
		if s.Bundle != nil {
			result = augmentWithPacks(result, s.Policy, *s.Bundle, normalized)
		}
	}
	return Replay{
		Snapshot: s,
		Receipt: Receipt{Version: 1, SnapshotSHA256: digestJSON(s),
			Changes: append([]Change{}, normalized...), MissingHistory: missingHistory, Recommendation: result},
	}, nil
}

func (r Replay) Verify(expectedSnapshot string) error {
	if r.Receipt.Version != 1 || r.Receipt.Changes == nil {
		return fmt.Errorf("invalid or unsupported receipt")
	}
	s, err := PrepareSnapshot(r.Snapshot)
	if err != nil {
		return err
	}
	if digestJSON(r.Snapshot) != digestJSON(s) {
		return fmt.Errorf("receipt snapshot is not canonical")
	}
	if r.Receipt.SnapshotSHA256 != digestJSON(s) ||
		(expectedSnapshot != "" && expectedSnapshot != r.Receipt.SnapshotSHA256) {
		return fmt.Errorf("receipt snapshot digest mismatch")
	}
	replay, err := s.Evaluate(r.Receipt.Changes, r.Receipt.MissingHistory)
	if err != nil {
		return err
	}
	if !slices.Equal(replay.Receipt.Changes, r.Receipt.Changes) ||
		digestJSON(replay.Receipt.Recommendation) != digestJSON(r.Receipt.Recommendation) {
		return fmt.Errorf("receipt recommendation differs from replay")
	}
	return nil
}
