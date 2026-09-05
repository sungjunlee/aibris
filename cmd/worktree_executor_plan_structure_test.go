package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWorktreeExecutorPlanLivesApartFromExecutorEntry(t *testing.T) {
	planNames := []string{
		"prepareCleanExecutionWithSafety",
		"prepareCleanExecutionWithOptions",
	}
	executorNames := []string{
		"executeCleanTargets",
		"executePreparedCleanTargets",
		"executePathCleanupTarget",
		"executeActiveWorktreeUnit",
		"defaultActiveWorktreeExecutionOptions",
	}

	wanted := make(map[string]string, len(planNames)+len(executorNames))
	for _, name := range planNames {
		wanted[name] = "worktree_executor_plan.go"
	}
	for _, name := range executorNames {
		wanted[name] = "worktree_executor.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cmd: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
					continue
				}
				if _, ok := wanted[fn.Name.Name]; !ok {
					continue
				}
				owners[fn.Name.Name] = append(owners[fn.Name.Name], base)
			}
		}
	}

	for name, owner := range wanted {
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestWorktreeExecutorPlanReexportIdentity(t *testing.T) {
	// Same-package names stay the plan implementations, so existing cmd call
	// sites keep resolving without import-path changes.
	if reflect.ValueOf(prepareCleanExecutionWithSafety).Pointer() == 0 {
		t.Fatal("prepareCleanExecutionWithSafety has no implementation")
	}
	if reflect.ValueOf(prepareCleanExecutionWithOptions).Pointer() == 0 {
		t.Fatal("prepareCleanExecutionWithOptions has no implementation")
	}

	executorSource := readCmdSource(t, "worktree_executor.go")
	for _, name := range []string{
		"prepareCleanExecutionWithSafety",
		"prepareCleanExecutionWithOptions",
	} {
		if strings.Contains(executorSource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_executor.go", name)
		}
	}
	if !strings.Contains(executorSource, "prepareCleanExecutionWithSafety(") {
		t.Error("worktree_executor.go no longer calls prepareCleanExecutionWithSafety")
	}
}
