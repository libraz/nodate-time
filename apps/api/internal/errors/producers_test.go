package apierrors

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pendingProducers are Specs declared ahead of the handlers that return them.
// The list is held from both sides: an entry that gains a producer fails as
// stale, so it shrinks to empty as the handlers land rather than collecting
// forgotten promises.
var pendingProducers = map[string]bool{
	"EventExceptionNotFound": true,
	"EventNotDeleted":        true,
	"EventNotRecurring":      true,
	"EventRestoreConflict":   true,
	"EventRestoreExpired":    true,
	"SearchTooBroad":         true,
}

// A declared error that no path returns is a promise the client is written
// against and the server never keeps. Every Spec must be referenced from
// non-test code outside this package, unless it is listed as pending.
func TestEveryDeclaredSpecHasAProducer(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "errors.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var specs []string
	ast.Inspect(file, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, v := range vs.Values {
			if u, ok := v.(*ast.UnaryExpr); ok {
				if lit, ok := u.X.(*ast.CompositeLit); ok {
					if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "Spec" {
						specs = append(specs, vs.Names[i].Name)
					}
				}
			}
		}
		return true
	})
	if len(specs) == 0 {
		t.Fatal("no Spec declarations found in errors.go")
	}

	var source strings.Builder
	root := filepath.Join("..", "..")
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (filepath.Base(path) == "errors" || filepath.Base(path) == "tests") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	all := source.String()
	declared := map[string]bool{}
	for _, name := range specs {
		declared[name] = true
		produced := strings.Contains(all, "apierrors."+name)
		switch {
		case !produced && !pendingProducers[name]:
			t.Errorf("%s is declared but nothing returns it", name)
		case produced && pendingProducers[name]:
			t.Errorf("%s now has a producer; remove it from pendingProducers", name)
		}
	}
	for name := range pendingProducers {
		if !declared[name] {
			t.Errorf("pendingProducers lists %s, which errors.go does not declare", name)
		}
	}
}
