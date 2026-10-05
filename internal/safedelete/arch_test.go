package safedelete

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoRemoveAllOutsideTheGate keeps this package the only place that can
// recursively delete. Outside it, production code may not reference
// os.RemoveAll at all. An os.Root can delete too, so os.OpenRoot is allowed
// only where strip opens a unit, which removes through RemoveAllIn.
func TestNoRemoveAllOutsideTheGate(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "go.mod")); err != nil {
		t.Fatalf("repository root not found: %v", err)
	}
	gate := filepath.Join(repo, "internal", "safedelete")
	openRootAllowed := filepath.Join(repo, "internal", "worktree", "strip.go")
	err = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path == gate || path == filepath.Join(repo, "tools") ||
				(path != repo && strings.HasPrefix(name, ".")) || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, path)
		osNames := map[string]bool{}
		for _, spec := range file.Imports {
			if importPath, err := strconv.Unquote(spec.Path.Value); err != nil || importPath != "os" {
				continue
			}
			name := "os"
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name == "." {
				t.Errorf("%s: package os dot-imported; use a named import", rel)
			}
			osNames[name] = true
		}
		if len(osNames) == 0 {
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if recv, ok := sel.X.(*ast.Ident); ok && osNames[recv.Name] {
				switch {
				case sel.Sel.Name == "RemoveAll":
					t.Errorf("%s: os.RemoveAll outside internal/safedelete", rel)
				case sel.Sel.Name == "OpenRoot" && path != openRootAllowed:
					t.Errorf("%s: os.OpenRoot outside internal/safedelete and strip", rel)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
