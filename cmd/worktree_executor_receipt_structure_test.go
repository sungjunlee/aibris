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

func TestWorktreeExecutorReceiptLivesApartFromExecutorEntry(t *testing.T) {
	receiptNames := []string{
		"applyActiveUnitExecutionReceipt",
		"setActiveReceiptPhysicalState",
		"failedCleanUnitReceipt",
		"failedPreparedCleanUnitReceipt",
		"cancelledPreparedCleanUnitReceipt",
		"newCleanUnitExecutionReceipt",
		"applyOverlapValidationReceipt",
		"cleanUnitHasMutation",
	}
	receiptTypes := []string{
		"cleanExecutionState",
		"cleanMemberExecutionReceipt",
		"cleanUnitExecutionReceipt",
		"cleanExecutionReceipt",
	}
	executorNames := []string{
		"executeCleanTargets",
		"defaultActiveWorktreeExecutionOptions",
	}
	preparedNames := []string{
		"executePreparedCleanTargets",
	}

	wanted := make(map[string]string, len(receiptNames)+len(receiptTypes)+len(executorNames)+len(preparedNames))
	for _, name := range receiptNames {
		wanted[name] = "worktree_executor_receipt.go"
	}
	for _, name := range receiptTypes {
		wanted[name] = "worktree_executor_receipt.go"
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
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv != nil {
						continue
					}
					if _, ok := wanted[d.Name.Name]; !ok {
						continue
					}
					owners[d.Name.Name] = append(owners[d.Name.Name], base)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if _, ok := wanted[ts.Name.Name]; !ok {
							continue
						}
						owners[ts.Name.Name] = append(owners[ts.Name.Name], base)
					}
				}
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

func TestWorktreeExecutorReceiptReexportIdentity(t *testing.T) {
	// Same-package names stay the receipt implementations, so existing cmd call
	// sites keep resolving without import-path changes.
	if reflect.ValueOf(applyActiveUnitExecutionReceipt).Pointer() == 0 {
		t.Fatal("applyActiveUnitExecutionReceipt has no implementation")
	}
	if reflect.ValueOf(setActiveReceiptPhysicalState).Pointer() == 0 {
		t.Fatal("setActiveReceiptPhysicalState has no implementation")
	}
	if reflect.ValueOf(failedCleanUnitReceipt).Pointer() == 0 {
		t.Fatal("failedCleanUnitReceipt has no implementation")
	}
	if reflect.ValueOf(failedPreparedCleanUnitReceipt).Pointer() == 0 {
		t.Fatal("failedPreparedCleanUnitReceipt has no implementation")
	}
	if reflect.ValueOf(cancelledPreparedCleanUnitReceipt).Pointer() == 0 {
		t.Fatal("cancelledPreparedCleanUnitReceipt has no implementation")
	}
	if reflect.ValueOf(newCleanUnitExecutionReceipt).Pointer() == 0 {
		t.Fatal("newCleanUnitExecutionReceipt has no implementation")
	}
	if reflect.ValueOf(applyOverlapValidationReceipt).Pointer() == 0 {
		t.Fatal("applyOverlapValidationReceipt has no implementation")
	}
	if reflect.ValueOf(cleanUnitHasMutation).Pointer() == 0 {
		t.Fatal("cleanUnitHasMutation has no implementation")
	}

	executorSource := readCmdSource(t, "worktree_executor.go")
	preparedSource := readCmdSource(t, "worktree_executor_prepared.go")
	receiptSource := readCmdSource(t, "worktree_executor_receipt.go")
	for _, name := range []string{
		"applyActiveUnitExecutionReceipt",
		"setActiveReceiptPhysicalState",
		"failedCleanUnitReceipt",
		"failedPreparedCleanUnitReceipt",
		"cancelledPreparedCleanUnitReceipt",
		"newCleanUnitExecutionReceipt",
		"applyOverlapValidationReceipt",
		"cleanUnitHasMutation",
	} {
		if strings.Contains(executorSource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_executor.go", name)
		}
		if strings.Contains(preparedSource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_executor_prepared.go", name)
		}
		if !strings.Contains(receiptSource, "func "+name+"(") {
			t.Errorf("%s is not defined in worktree_executor_receipt.go", name)
		}
	}
	for _, name := range []string{
		"cleanExecutionState",
		"cleanMemberExecutionReceipt",
		"cleanUnitExecutionReceipt",
		"cleanExecutionReceipt",
	} {
		if strings.Contains(executorSource, "type "+name+" ") || strings.Contains(executorSource, "type "+name+" struct") {
			t.Errorf("%s is still defined in worktree_executor.go", name)
		}
		if strings.Contains(preparedSource, "type "+name+" ") || strings.Contains(preparedSource, "type "+name+" struct") {
			t.Errorf("%s is still defined in worktree_executor_prepared.go", name)
		}
		if !strings.Contains(receiptSource, "type "+name+" ") && !strings.Contains(receiptSource, "type "+name+" struct") {
			t.Errorf("%s is not defined in worktree_executor_receipt.go", name)
		}
	}
	if !strings.Contains(receiptSource, "func (r cleanExecutionReceipt) counts(") {
		t.Error("counts is not defined on cleanExecutionReceipt in worktree_executor_receipt.go")
	}
	if strings.Contains(executorSource, "func (r cleanExecutionReceipt) counts(") {
		t.Error("counts is still defined in worktree_executor.go")
	}
	if strings.Contains(preparedSource, "func (r cleanExecutionReceipt) counts(") {
		t.Error("counts is still defined in worktree_executor_prepared.go")
	}
	for _, name := range []string{
		"cancelledPreparedCleanUnitReceipt(",
		"failedPreparedCleanUnitReceipt(",
		"cleanUnitHasMutation(",
	} {
		if !strings.Contains(preparedSource, name) {
			t.Errorf("worktree_executor_prepared.go no longer calls %s", strings.TrimSuffix(name, "("))
		}
	}
}
