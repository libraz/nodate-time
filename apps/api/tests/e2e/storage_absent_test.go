package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libraz/nodate-time/apps/api/tests/helpers"
)

// storageAbsentPrefix names the tests that can only observe what they are
// about — the 503 the API answers when no object storage is configured — in a
// run that has no storage at all. The test server is built once per process,
// so those tests cannot share a run with the storage suite; they are selected
// out of the package by this prefix instead.
const storageAbsentPrefix = "TestStorageAbsent"

// storageAbsentTarget is the make target that runs them, and the only place
// the selector regex is written down.
const storageAbsentTarget = "test-e2e-storage-absent"

// requireStorageAbsent is the opposite of requireStorage: it skips a test that
// is about the storage-less response when the run does have storage. The
// message names the target because a skip here means the run is incomplete,
// not that there was nothing to check.
func requireStorageAbsent(t *testing.T) {
	t.Helper()
	if helpers.StorageEnabled() {
		t.Skip("only observable without storage -- run: make " + storageAbsentTarget)
	}
}

// TestStorageOptOutConvention keeps the selector honest. Selecting tests by
// name works only while the name and the opt-out agree, and nothing else in
// the pipeline can tell they have drifted: a test that opts out of storage
// under some other name skips in the storage run, is not selected by the
// storage-absent run, and so never executes anywhere while both runs stay
// green. This asserts the agreement in both directions, that the set is not
// empty, and that the make target still selects on the same prefix.
func TestStorageOptOutConvention(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse package sources: %v", err)
	}

	var selected []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(fn.Name.Name, "Test") {
					continue
				}
				name := fn.Name.Name
				where := fset.Position(fn.Pos())
				optsOut := callsIdent(fn.Body, "requireStorageAbsent")
				named := strings.HasPrefix(name, storageAbsentPrefix)

				if inlineStorageAbsentSkip(fn.Body) {
					t.Errorf("%s: %s skips itself when storage is enabled by hand; call requireStorageAbsent(t) so the convention check can see it",
						where, name)
					continue
				}
				switch {
				case optsOut && !named:
					t.Errorf("%s: %s calls requireStorageAbsent but is not named %s*, so `make %s` never runs it",
						where, name, storageAbsentPrefix, storageAbsentTarget)
				case named && !optsOut:
					t.Errorf("%s: %s is named %s* but does not call requireStorageAbsent, so it also runs in the storage suite",
						where, name, storageAbsentPrefix)
				case optsOut:
					selected = append(selected, name)
				}
			}
		}
	}

	if len(selected) == 0 {
		t.Fatalf("no test opts out of storage; `make %s` would run nothing and still pass", storageAbsentTarget)
	}

	assertMakeTargetSelectsPrefix(t)
}

// assertMakeTargetSelectsPrefix reads the recipe of the make target and checks
// it still filters on storageAbsentPrefix. Renaming the convention in Go alone
// would otherwise leave the target matching nothing, which go test reports as
// success.
func assertMakeTargetSelectsPrefix(t *testing.T) {
	t.Helper()
	path := findMakefile(t)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	var recipe []string
	inTarget := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, storageAbsentTarget+":") {
			inTarget = true
			continue
		}
		if !inTarget {
			continue
		}
		if line != "" && !strings.HasPrefix(line, "\t") {
			break
		}
		recipe = append(recipe, line)
	}
	if len(recipe) == 0 {
		t.Fatalf("%s: no recipe for target %q", path, storageAbsentTarget)
	}
	if !strings.Contains(strings.Join(recipe, "\n"), storageAbsentPrefix) {
		t.Errorf("%s: target %q no longer selects on %q, so it runs a different set than this package declares",
			path, storageAbsentTarget, storageAbsentPrefix)
	}
}

// findMakefile walks up from the package directory to the repository root.
func findMakefile(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "Makefile")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("no Makefile above %s", dir)
	return ""
}

// callsIdent reports whether the body calls the named package-level function.
func callsIdent(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
			found = true
		}
		return !found
	})
	return found
}

// inlineStorageAbsentSkip reports whether the body contains the hand-written
// shape requireStorageAbsent replaces: `if helpers.StorageEnabled() { skip }`.
// The negated form is the storage-required direction and is left alone.
func inlineStorageAbsentSkip(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		stmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		call, ok := stmt.Cond.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "StorageEnabled" {
			return true
		}
		if callsSkip(stmt.Body) {
			found = true
		}
		return !found
	})
	return found
}

// callsSkip reports whether the block calls one of testing.T's skip methods.
func callsSkip(block *ast.BlockStmt) bool {
	found := false
	ast.Inspect(block, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Skip", "Skipf", "SkipNow":
			found = true
		}
		return !found
	})
	return found
}
