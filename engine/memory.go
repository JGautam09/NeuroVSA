package engine

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"

	"neuro-vsa/core"
)

// AssociativeMemory stores hypervector sequence associations in memory and on disk.
type AssociativeMemory struct {
	mu           sync.RWMutex
	MemoryMatrix core.Hypervector
	History      []core.Hypervector
}

// NewAssociativeMemory initializes an empty associative memory instance.
func NewAssociativeMemory() *AssociativeMemory {
	return &AssociativeMemory{
		MemoryMatrix: core.ZeroHV(),
		History:      make([]core.Hypervector, 0),
	}
}

// StoreAssociation binds a context vector with a target token vector and bundles it into memory matrix:
// V_memory = V_memory ⊕ (V_context ⊗ V_target)
func (am *AssociativeMemory) StoreAssociation(contextHV, targetHV core.Hypervector) {
	am.mu.Lock()
	defer am.mu.Unlock()

	bound := contextHV.Bind(targetHV)
	am.History = append(am.History, bound)
	am.MemoryMatrix = core.Bundle(am.History)
}

// SaveToFile serializes the memory matrix and history vectors to disk.
func (am *AssociativeMemory) SaveToFile(filename string) error {
	am.mu.RLock()
	defer am.mu.RUnlock()

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create memory file: %w", err)
	}
	defer f.Close()

	// Write memory matrix (157 uint64 words = 1256 bytes)
	for i := 0; i < core.NumWords; i++ {
		if err := binary.Write(f, binary.LittleEndian, am.MemoryMatrix.Vector[i]); err != nil {
			return err
		}
	}

	// Write history count (uint32)
	count := uint32(len(am.History))
	if err := binary.Write(f, binary.LittleEndian, count); err != nil {
		return err
	}

	// Write history vectors
	for _, hv := range am.History {
		for i := 0; i < core.NumWords; i++ {
			if err := binary.Write(f, binary.LittleEndian, hv.Vector[i]); err != nil {
				return err
			}
		}
	}

	return nil
}

// LoadFromFile deserializes memory matrix and history from disk.
func (am *AssociativeMemory) LoadFromFile(filename string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open memory file: %w", err)
	}
	defer f.Close()

	var memHV core.Hypervector
	for i := 0; i < core.NumWords; i++ {
		if err := binary.Read(f, binary.LittleEndian, &memHV.Vector[i]); err != nil {
			return err
		}
	}
	am.MemoryMatrix = memHV

	var count uint32
	if err := binary.Read(f, binary.LittleEndian, &count); err != nil {
		if err == io.EOF {
			am.History = make([]core.Hypervector, 0)
			return nil
		}
		return err
	}

	history := make([]core.Hypervector, count)
	for h := 0; h < int(count); h++ {
		var hv core.Hypervector
		for i := 0; i < core.NumWords; i++ {
			if err := binary.Read(f, binary.LittleEndian, &hv.Vector[i]); err != nil {
				return err
			}
		}
		history[h] = hv
	}
	am.History = history

	return nil
}
