package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"

	"github.com/JGautam09/NeuroVSA/core"
)

// MaxIndexFiles caps how many .go files IndexDirectory will process, bounding the work an
// indexing request can trigger on a large or adversarial directory tree.
const MaxIndexFiles = 5000

// CodeASTIndexer parses Go source files and encodes function/struct AST trees into hyperdimensional vectors.
type CodeASTIndexer struct {
	Dict *core.TokenDictionary
}

// FunctionASTVector represents an encoded function declaration's structural hypervector.
type FunctionASTVector struct {
	FuncName string
	FilePath string
	ASTHV    core.Hypervector
}

// NewCodeASTIndexer creates a new indexer instance with a shared TokenDictionary.
func NewCodeASTIndexer(dict *core.TokenDictionary) *CodeASTIndexer {
	if dict == nil {
		dict = core.NewTokenDictionary()
	}
	return &CodeASTIndexer{Dict: dict}
}

// IndexFile parses a single Go source file and encodes its declarations into hypervectors.
func (indexer *CodeASTIndexer) IndexFile(filePath string) ([]FunctionASTVector, core.Hypervector, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, core.ZeroHV(), err
	}

	var funcVectors []FunctionASTVector
	var fileComponents []core.Hypervector

	ast.Inspect(node, func(n ast.Node) bool {
		fn, isFunc := n.(*ast.FuncDecl)
		if !isFunc {
			return true
		}

		funcName := fn.Name.Name
		funcHV := indexer.Dict.GetOrRegister("func:" + funcName)

		components := []core.Hypervector{funcHV}
		paramIndex := 1

		// Encode function parameters with position permutation ρ^pos(V_param)
		if fn.Type.Params != nil {
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					paramHV := indexer.Dict.GetOrRegister("param:" + name.Name)
					permutedParamHV := paramHV.Permute(paramIndex)
					components = append(components, permutedParamHV)
					paramIndex++
				}
			}
		}

		// Encode return types with position permutation
		if fn.Type.Results != nil {
			for _, field := range fn.Type.Results.List {
				if len(field.Names) > 0 {
					for _, name := range field.Names {
						retHV := indexer.Dict.GetOrRegister("return:" + name.Name)
						permutedRetHV := retHV.Permute(paramIndex)
						components = append(components, permutedRetHV)
						paramIndex++
					}
				}
			}
		}

		// Bundle function components: V_AST = V_FuncName ⊕ ρ^1(V_Param1) ⊕ ρ^2(V_Param2) ...
		astHV := core.Bundle(components)

		fnVec := FunctionASTVector{
			FuncName: funcName,
			FilePath: filePath,
			ASTHV:    astHV,
		}

		funcVectors = append(funcVectors, fnVec)
		fileComponents = append(fileComponents, astHV)
		return true
	})

	fileHV := core.Bundle(fileComponents)
	return funcVectors, fileHV, nil
}

// IndexDirectory traverses a directory recursively and indexes all .go source files.
func (indexer *CodeASTIndexer) IndexDirectory(dirPath string) (map[string]core.Hypervector, []FunctionASTVector, error) {
	fileMap := make(map[string]core.Hypervector)
	var allFuncs []FunctionASTVector
	indexed := 0

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".go" {
			indexed++
			if indexed > MaxIndexFiles {
				return fmt.Errorf("aborted: directory exceeds the %d .go-file index limit", MaxIndexFiles)
			}
			funcs, fileHV, parseErr := indexer.IndexFile(path)
			if parseErr == nil {
				fileMap[path] = fileHV
				allFuncs = append(allFuncs, funcs...)
			}
		}
		return nil
	})

	return fileMap, allFuncs, err
}
