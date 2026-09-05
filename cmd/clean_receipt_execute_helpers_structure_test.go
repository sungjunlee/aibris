package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = cleanJSONReceiptTargetIDsForPrepared
	_ = orderCleanJSONReceiptPreparedTargets
	_ = rejectCleanJSONReceiptTargetSetMismatch
	_ = containsPreparedCleanJSONTarget
)

func TestCleanJSONReceiptExecuteHelpersLiveApartFromExecuteEntry(t *testing.T) {
	helperNames := []string{
		"cleanJSONReceiptTargetIDsForPrepared",
		"orderCleanJSONReceiptPreparedTargets",
		"rejectCleanJSONReceiptTargetSetMismatch",
		"containsPreparedCleanJSONTarget",
	}
	executeNames := []string{
		"executeCleanJSONReceipt",
		"executeInteractiveCleanJSONReceipt",
		"applyCleanJSONExecutionReceipt",
		"finishCleanJSONReceipt",
		"finalizeCleanJSONReceipt",
	}

	wanted := make(map[string]string, len(helperNames)+len(executeNames))
	for _, name := range helperNames {
		wanted[name] = "clean_receipt_execute_helpers.go"
	}
	for _, name := range executeNames {
		wanted[name] = "clean_receipt_execute.go"
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
