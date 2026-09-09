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

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = executePathCleanupTarget
	_ = executeActiveWorktreeUnit
	_ = isActiveWorktreeTarget
	_ = pathDoesNotExist
	_ = debrisExecutionName
)

func TestWorktreeExecutorHelpersLiveApartFromExecutorEntry(t *testing.T) {
	helperNames := []string{
		"executePathCleanupTarget",
		"executeActiveWorktreeUnit",
		"isActiveWorktreeTarget",
		"pathDoesNotExist",
		"debrisExecutionName",
	}
	executorNames := []string{
		"executeCleanTargets",
		"defaultActiveWorktreeExecutionOptions",
	}
	preparedNames := []string{
		"executePreparedCleanTargets",
	}

	wanted := make(map[string]string, len(helperNames)+len(executorNames)+len(preparedNames))
	for _, name := range helperNames {
		wanted[name] = "worktree_executor_helpers.go"
	}
	for _, name := range executorNames {
		wanted[name] = "worktree_executor.go"
	}
	for _, name := range preparedNames {
		wanted[name] = "worktree_executor_prepared.go"
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

func TestWorktreeExecutorHelpersReexportIdentity(t *testing.T) {
	// Same-package names stay the helper implementations, so existing cmd call
	// sites keep resolving without import-path changes.
	if reflect.ValueOf(executePathCleanupTarget).Pointer() == 0 {
		t.Fatal("executePathCleanupTarget has no implementation")
	}
	if reflect.ValueOf(executeActiveWorktreeUnit).Pointer() == 0 {
		t.Fatal("executeActiveWorktreeUnit has no implementation")
	}
	if reflect.ValueOf(isActiveWorktreeTarget).Pointer() == 0 {
		t.Fatal("isActiveWorktreeTarget has no implementation")
	}
	if reflect.ValueOf(pathDoesNotExist).Pointer() == 0 {
		t.Fatal("pathDoesNotExist has no implementation")
	}
	if reflect.ValueOf(debrisExecutionName).Pointer() == 0 {
		t.Fatal("debrisExecutionName has no implementation")
	}

	executorSource := readCmdSource(t, "worktree_executor.go")
	preparedSource := readCmdSource(t, "worktree_executor_prepared.go")
	helperSource := readCmdSource(t, "worktree_executor_helpers.go")
	for _, name := range []string{
		"executePathCleanupTarget",
		"executeActiveWorktreeUnit",
		"isActiveWorktreeTarget",
		"pathDoesNotExist",
		"debrisExecutionName",
	} {
		if strings.Contains(executorSource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_executor.go", name)
		}
		if strings.Contains(preparedSource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_executor_prepared.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in worktree_executor_helpers.go", name)
		}
	}
	for _, name := range []string{
		"executePathCleanupTarget(",
		"executeActiveWorktreeUnit(",
		"isActiveWorktreeTarget(",
	} {
		if !strings.Contains(preparedSource, name) {
			t.Errorf("worktree_executor_prepared.go no longer calls %s", strings.TrimSuffix(name, "("))
		}
	}
	for _, name := range []string{
		"newCleanUnitExecutionReceipt(",
		"applyOverlapValidationReceipt(",
		"applyActiveUnitExecutionReceipt(",
		"setActiveReceiptPhysicalState(",
		"pathDoesNotExist(",
		"debrisExecutionName(",
	} {
		if !strings.Contains(helperSource, name) {
			t.Errorf("worktree_executor_helpers.go no longer calls %s", strings.TrimSuffix(name, "("))
		}
	}
}
