//go:build darwin || linux

package engine

import (
	"fmt"
	"os"
	"syscall"
)

// writeMappedFile creates (or truncates) path to exactly size bytes, memory-maps it
// read/write, and lets fill populate the mapped region directly — so the serialized image
// is written straight into the file's page cache with no intermediate heap buffer. Durability
// is forced with fsync (stdlib syscall does not expose Msync on darwin, and fsync flushes the
// same inode pages that the shared mapping dirtied).
func writeMappedFile(path string, size int, fill func(buf []byte)) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create memory file: %w", err)
	}
	defer f.Close()

	if err := f.Truncate(int64(size)); err != nil {
		return fmt.Errorf("failed to size memory file: %w", err)
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("mmap (write) failed: %w", err)
	}

	fill(data)

	if err := syscall.Munmap(data); err != nil {
		return fmt.Errorf("munmap failed: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync failed: %w", err)
	}
	return nil
}

// openMappedFile memory-maps path read-only and returns the mapped bytes together with a
// closer that unmaps them. The fd may be closed immediately; the mapping stays valid until
// unmapped.
func openMappedFile(path string) ([]byte, func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	size := int(info.Size())
	if size == 0 {
		return nil, nil, fmt.Errorf("memory file %q is empty", path)
	}

	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("mmap (read) failed: %w", err)
	}
	return data, func() error { return syscall.Munmap(data) }, nil
}
