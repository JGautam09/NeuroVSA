package parser

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"

	"github.com/JGautam09/NeuroVSA/core"
)

// MaxIndexFiles caps how many .go files IndexDirectory will process, bounding the work an
// indexing request can trigger on a large or adversarial directory tree.
const MaxIndexFiles = 5000

// EncoderV1 encodes identifier NAMES only (function name, param/return names) — the
// original encoder, kept for comparison and compatibility. EncoderV2 adds structure:
// receiver/param/return TYPES role-bound to names, statement KINDS, and a position-
// permuted control-flow stream, so functions with the same shape match even when every
// identifier is renamed. The measured difference is in BENCHMARKS.md; both are
// deterministic under a seeded dictionary.
const (
	EncoderV1 = 1
	EncoderV2 = 2
)

// MaxStmtStream bounds the control-flow stream per function: statement kinds beyond this
// many (walked in source order, nested blocks included) are ignored. Keeps encoding O(1)
// per pathological function.
const MaxStmtStream = 64

// CodeASTIndexer parses Go source files and encodes function/struct AST trees into hyperdimensional vectors.
type CodeASTIndexer struct {
	Dict *core.TokenDictionary
	// Version selects the encoding (EncoderV1 or EncoderV2). The zero value means
	// EncoderV1, so existing constructors keep their exact historical behavior.
	Version int
}

// FunctionASTVector represents an encoded function declaration's structural hypervector.
type FunctionASTVector struct {
	FuncName string
	FilePath string
	ASTHV    core.Hypervector
}

// NewCodeASTIndexer creates a new indexer instance with a shared TokenDictionary,
// using the original names-only EncoderV1.
func NewCodeASTIndexer(dict *core.TokenDictionary) *CodeASTIndexer {
	if dict == nil {
		dict = core.NewTokenDictionary()
	}
	return &CodeASTIndexer{Dict: dict, Version: EncoderV1}
}

// NewCodeASTIndexerV2 creates an indexer using the structural EncoderV2.
func NewCodeASTIndexerV2(dict *core.TokenDictionary) *CodeASTIndexer {
	idx := NewCodeASTIndexer(dict)
	idx.Version = EncoderV2
	return idx
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

		var astHV core.Hypervector
		if indexer.Version == EncoderV2 {
			astHV = indexer.encodeFuncV2(fn)
		} else {
			astHV = indexer.encodeFuncV1(fn)
		}

		fnVec := FunctionASTVector{
			FuncName: fn.Name.Name,
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

// encodeFuncV1 is the original names-only encoding:
// V_AST = V_FuncName ⊕ ρ^1(V_Param1) ⊕ ρ^2(V_Param2) ... (+ named returns).
func (indexer *CodeASTIndexer) encodeFuncV1(fn *ast.FuncDecl) core.Hypervector {
	components := []core.Hypervector{indexer.Dict.GetOrRegister("func:" + fn.Name.Name)}
	paramIndex := 1

	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				components = append(components, indexer.Dict.GetOrRegister("param:"+name.Name).Permute(paramIndex))
				paramIndex++
			}
		}
	}
	if fn.Type.Results != nil {
		for _, field := range fn.Type.Results.List {
			for _, name := range field.Names {
				components = append(components, indexer.Dict.GetOrRegister("return:"+name.Name).Permute(paramIndex))
				paramIndex++
			}
		}
	}
	return core.Bundle(components)
}

// encodeFuncV2 adds structure to the name signal, so functions with the same SHAPE match
// even when every identifier is renamed:
//
//   - receiver TYPE (methods): role:recv ⊗ type:T
//   - params: ρ^pos(param-name ⊗ type:T) — name and type role-bound into one slot token
//     (unnamed params contribute the type alone)
//   - returns: ρ^pos(rettype:T) — return TYPES, positional (v1 only saw the rare named returns)
//   - control-flow stream: ρ^(100+i)(stmt:kind) for the first MaxStmtStream statement
//     kinds in source order (nested blocks included). The +100 offset keeps statement
//     positions in a different permutation space than signature positions.
//
// Everything still bundles by majority vote, so shared structure raises similarity
// smoothly rather than requiring exact matches anywhere.
func (indexer *CodeASTIndexer) encodeFuncV2(fn *ast.FuncDecl) core.Hypervector {
	components := []core.Hypervector{indexer.Dict.GetOrRegister("func:" + fn.Name.Name)}

	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvT := indexer.Dict.GetOrRegister("type:" + exprString(fn.Recv.List[0].Type))
		components = append(components, indexer.Dict.GetOrRegister("role:recv").Bind(recvT))
	}

	pos := 1
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			tHV := indexer.Dict.GetOrRegister("type:" + exprString(field.Type))
			if len(field.Names) == 0 { // unnamed parameter: the type carries the slot
				components = append(components, tHV.Permute(pos))
				pos++
				continue
			}
			for _, name := range field.Names {
				nameHV := indexer.Dict.GetOrRegister("param:" + name.Name)
				components = append(components, nameHV.Bind(tHV).Permute(pos))
				pos++
			}
		}
	}
	if fn.Type.Results != nil {
		for _, field := range fn.Type.Results.List {
			tHV := indexer.Dict.GetOrRegister("rettype:" + exprString(field.Type))
			slots := len(field.Names)
			if slots == 0 {
				slots = 1
			}
			for i := 0; i < slots; i++ {
				components = append(components, tHV.Permute(pos))
				pos++
			}
		}
	}

	if fn.Body != nil {
		// A hard node budget that TERMINATES traversal — not ast.Inspect, whose `return false`
		// only prunes a node's children and keeps walking later siblings, so a huge flat
		// function would still be visited end to end. streamStmtKinds stops the moment the
		// budget is spent, making encoding genuinely bounded per function.
		stream := streamStmtKinds(fn.Body, MaxStmtStream)
		for i, kind := range stream {
			components = append(components, indexer.Dict.GetOrRegister("stmt:"+kind).Permute(100+(i+1)))
		}
	}
	return core.Bundle(components)
}

// streamStmtKinds walks a function body in source order and returns up to `limit` statement
// KIND tokens, terminating traversal completely once the limit is reached (a global budget,
// not per-subtree pruning). It descends only into the block-bearing statements that define
// control-flow shape — bodies of if/for/range/switch/select and nested blocks — which is
// enough to characterize structure without an unbounded full-tree walk. O(min(nodes, limit)).
func streamStmtKinds(body *ast.BlockStmt, limit int) []string {
	out := make([]string, 0, limit)
	var walk func(stmts []ast.Stmt)
	walk = func(stmts []ast.Stmt) {
		for _, s := range stmts {
			if len(out) >= limit {
				return
			}
			if kind := stmtKind(s); kind != "" {
				out = append(out, kind)
			}
			if len(out) >= limit {
				return
			}
			switch n := s.(type) {
			case *ast.BlockStmt:
				walk(n.List)
			case *ast.IfStmt:
				if n.Body != nil {
					walk(n.Body.List)
				}
				if n.Else != nil {
					walk([]ast.Stmt{n.Else})
				}
			case *ast.ForStmt:
				if n.Body != nil {
					walk(n.Body.List)
				}
			case *ast.RangeStmt:
				if n.Body != nil {
					walk(n.Body.List)
				}
			case *ast.SwitchStmt:
				if n.Body != nil {
					walk(n.Body.List)
				}
			case *ast.TypeSwitchStmt:
				if n.Body != nil {
					walk(n.Body.List)
				}
			case *ast.SelectStmt:
				if n.Body != nil {
					walk(n.Body.List)
				}
			case *ast.CaseClause:
				walk(n.Body)
			case *ast.CommClause:
				walk(n.Body)
			case *ast.LabeledStmt:
				walk([]ast.Stmt{n.Stmt})
			}
		}
	}
	walk(body.List)
	return out
}

// stmtKind maps an AST node to its control-flow token ("" for nodes that are not
// statements we track). Deliberately coarse: KINDS in ORDER are the structural signal,
// not expression contents.
func stmtKind(n ast.Node) string {
	switch s := n.(type) {
	case *ast.IfStmt:
		return "if"
	case *ast.ForStmt:
		return "for"
	case *ast.RangeStmt:
		return "range"
	case *ast.SwitchStmt, *ast.TypeSwitchStmt:
		return "switch"
	case *ast.SelectStmt:
		return "select"
	case *ast.AssignStmt:
		return "assign"
	case *ast.ReturnStmt:
		return "return"
	case *ast.DeferStmt:
		return "defer"
	case *ast.GoStmt:
		return "go"
	case *ast.DeclStmt:
		return "decl"
	case *ast.ExprStmt:
		if _, isCall := s.X.(*ast.CallExpr); isCall {
			return "call"
		}
		return ""
	default:
		return ""
	}
}

// exprString renders a type expression ("[]int", "*Config", "map[string]int") for token
// registration. types.ExprString is stdlib and fset-free.
func exprString(e ast.Expr) string {
	return types.ExprString(e)
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
