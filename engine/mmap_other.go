//go:build !darwin && !linux

package engine

import "os"

// writeMappedFile is the portable fallback for platforms without the syscall.Mmap write
// path wired up here (e.g. Windows): it fills a heap buffer and writes it atomically.
func writeMappedFile(path string, size int, fill func(buf []byte)) error {
	buf := make([]byte, size)
	fill(buf)
	return os.WriteFile(path, buf, 0o644)
}

// openMappedFile is the portable fallback: it reads the whole file into a heap buffer. The
// returned closer is a no-op.
func openMappedFile(path string) ([]byte, func() error, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return data, func() error { return nil }, nil
}
