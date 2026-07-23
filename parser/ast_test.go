package parser

import (
	"os"
	"path/filepath"
	"testing"

	"neuro-vsa/core"
)

func TestIndexFile(t *testing.T) {
	tmpDir := t.TempDir()
	sampleFile := filepath.Join(tmpDir, "sample.go")

	code := `package main
import "fmt"

func Add(a int, b int) int {
	return a + b
}

func Multiply(x int, y int) int {
	return x * y
}
`

	err := os.WriteFile(sampleFile, []byte(code), 0644)
	if err != nil {
		t.Fatalf("Failed to write sample go file: %v", err)
	}

	dict := core.NewTokenDictionary()
	indexer := NewCodeASTIndexer(dict)

	funcVecs, fileHV, err := indexer.IndexFile(sampleFile)
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	if len(funcVecs) != 2 {
		t.Fatalf("Expected 2 functions indexed, got %d", len(funcVecs))
	}

	if funcVecs[0].FuncName != "Add" || funcVecs[1].FuncName != "Multiply" {
		t.Errorf("Unexpected function names indexed: %v", funcVecs)
	}

	if fileHV.IsZero() {
		t.Errorf("File hypervector should not be zero")
	}

	// Verify dictionary populated with func and param tokens
	if !dict.Contains("func:Add") || !dict.Contains("param:a") || !dict.Contains("func:Multiply") {
		t.Errorf("Dictionary missing expected AST tokens")
	}
}
