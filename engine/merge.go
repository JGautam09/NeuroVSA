package engine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/JGautam09/NeuroVSA/core"
)

// MergeReport summarizes what a Merge changed, so callers (e.g. a merge-worlds UI) can
// narrate it and surface the capacity warning.
type MergeReport struct {
	Added             int    // entries this memory had never seen
	Shared            int    // entries already present (identical content — shared history)
	TombstonesApplied int    // local active entries removed because the peer had forgotten them
	ActiveTotal       uint64 // active associations after the merge
	OverCapacity      bool   // ActiveTotal exceeds RecommendedMaxActiveAssociations (G0 envelope)
}

// Merge folds another memory's ledger into this one — the NeuroMesh operation. The ledger is
// a two-phase set keyed by globally unique (site, seq) IDs: merge is set union over entries
// plus OR over tombstones, with the tally and matrix rebuilt incrementally. By construction
// union is commutative, associative, and idempotent (proven by the property tests in
// merge_test.go), so any group of replicas that exchanges merges in any order converges to a
// bit-identical state (see Fingerprint).
//
// Contract:
//   - both memories must share a vocab seed (vectors must mean the same thing);
//   - distinct writers must use distinct sites — an incoming ID that exists locally with
//     DIFFERENT content is a site collision and aborts the merge with no partial effects;
//   - tombstones win and propagate: an association forgotten anywhere is forgotten everywhere
//     the merge reaches (the "right to be forgotten" travels with the data).
func (am *AssociativeMemory) Merge(other *AssociativeMemory) (MergeReport, error) {
	var rep MergeReport
	if am == other {
		am.mu.RLock()
		rep.Shared = len(am.ledger)
		rep.ActiveTotal = am.total
		rep.OverCapacity = am.total > RecommendedMaxActiveAssociations
		am.mu.RUnlock()
		return rep, nil
	}

	// Lock ordering by pointer-independent rule: take other as read, am as write. Distinct
	// instances (checked above), and Merge is the only cross-memory operation, so a fixed
	// acquire order (other first) cannot deadlock against another Merge taking (am, other)
	// concurrently — callers merging in both directions concurrently must serialize
	// externally; document over cleverness.
	other.mu.RLock()
	defer other.mu.RUnlock()
	am.mu.Lock()
	defer am.mu.Unlock()

	if am.readOnly {
		return rep, fmt.Errorf("cannot merge into a read-only (OpenReadOnly) memory")
	}
	if other.readOnly {
		return rep, fmt.Errorf("cannot merge from a read-only memory: its ledger is not loaded")
	}
	if am.vocabSeed != other.vocabSeed {
		return rep, fmt.Errorf("vocab seed mismatch (%d vs %d): memories from different vocabularies cannot merge — their vectors do not mean the same thing", am.vocabSeed, other.vocabSeed)
	}

	// Validation pass first: a site collision must abort with NO partial effects.
	for i := range other.ledger {
		oe := &other.ledger[i]
		if idx, ok := am.index[oe.id]; ok {
			le := &am.ledger[idx]
			if le.label != oe.label || le.bound != oe.bound {
				return rep, fmt.Errorf("association %s exists on both sides with different content — site collision or divergent writer history; refusing to merge", oe.id)
			}
		}
	}

	for i := range other.ledger {
		oe := &other.ledger[i]
		if idx, ok := am.index[oe.id]; ok {
			le := &am.ledger[idx]
			rep.Shared++
			// Tombstone propagation (monotone OR): forgetting wins.
			if oe.removed && !le.removed {
				am.subBits(le.bound)
				am.total--
				le.removed = true
				rep.TombstonesApplied++
			}
			continue
		}

		// New entry: adopt it (tombstoned entries arrive as tombstones — history without votes).
		am.index[oe.id] = len(am.ledger)
		am.ledger = append(am.ledger, ledgerEntry{id: oe.id, label: oe.label, removed: oe.removed, bound: oe.bound})
		if !oe.removed {
			am.addBits(oe.bound)
			am.total++
		}
		rep.Added++
		// If the peer somehow carries entries under OUR site (e.g. merging back an exported
		// copy of ourselves), advance the sequence so future stores cannot collide.
		if oe.id.Site == am.site && oe.id.Seq >= am.nextSeq {
			am.nextSeq = oe.id.Seq + 1
		}
	}

	am.rematerializeLocked()
	rep.ActiveTotal = am.total
	rep.OverCapacity = am.total > RecommendedMaxActiveAssociations
	return rep, nil
}

// Fingerprint returns a SHA-256 over the memory's CONVERGENT state: vocab seed, active
// total, and every ledger entry (including tombstones) in canonical order with its bound
// vector. Writer-local identity (site, nextSeq) is deliberately EXCLUDED — two replicas that
// have exchanged merges converge to equal fingerprints even though they remain distinct
// writers. Phase 3 decision certificates embed this hash as the memory-state anchor.
func (am *AssociativeMemory) Fingerprint() string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	h := sha256.New()
	h.Write([]byte("NVSA-FP1"))
	var buf [8]byte
	le := binary.LittleEndian
	le.PutUint64(buf[:], am.vocabSeed)
	h.Write(buf[:])
	le.PutUint64(buf[:], am.total)
	h.Write(buf[:])

	for _, e := range am.canonicalLocked() {
		le.PutUint64(buf[:], e.id.Site)
		h.Write(buf[:])
		le.PutUint64(buf[:], e.id.Seq)
		h.Write(buf[:])
		if e.removed {
			h.Write([]byte{1})
		} else {
			h.Write([]byte{0})
		}
		le.PutUint64(buf[:], uint64(len(e.label)))
		h.Write(buf[:])
		h.Write([]byte(e.label))
		for i := 0; i < core.NumWords; i++ {
			le.PutUint64(buf[:], e.bound.Vector[i])
			h.Write(buf[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
