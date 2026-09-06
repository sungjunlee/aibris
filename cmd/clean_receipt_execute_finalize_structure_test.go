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
	_ = applyCleanJSONExecutionReceipt
	_ = residualBytesJSON
	_ = cleanJSONReceiptStateReasons
	_ = finishCleanJSONReceipt
	_ = finalizeCleanJSONReceipt
	_ = newGuidedCleanExecutionReceipt
	_ = (*guidedCleanExecutionReceipt).observeInteractiveSkip
	_ = (*guidedCleanExecutionReceipt).finish
	_ = writeGuidedCleanExecutionReceipt
)

func TestCleanJSONReceiptExecuteFinalizeLivesApartFromExecuteEntry(t *testing.T) {
	finalizeNames := []string{
		"applyCleanJSONExecutionReceipt",
		"residualBytesJSON",
		"cleanJSONReceiptStateReasons",
		"finishCleanJSONReceipt",
		"finalizeCleanJSONReceipt",
		"newGuidedCleanExecutionReceipt",
		"observeInteractiveSkip",
		"finish",
		"writeGuidedCleanExecutionReceipt",
	}
	executeNames := []string{
		"executeCleanJSONReceipt",
		"executeInteractiveCleanJSONReceipt",
		"quietActiveWorktreeExecutionOptions",
		"readCleanJSONConfirmation",
		"scanCleanJSONInput",
		"markPreparedCleanJSONReceiptTargets",
		"markCleanJSONReceiptTarget",
	}
	finalizeTypes := []string{
		"guidedCleanExecutionReceipt",
	}

	wanted := make(map[string]string, len(finalizeNames)+len(executeNames)+len(finalizeTypes))
	for _, name := range finalizeNames {
		wanted[name] = "clean_receipt_execute_finalize.go"
	}
	for _, name := range executeNames {
		wanted[name] = "clean_receipt_execute.go"
	}
	for _, name := range finalizeTypes {
		wanted[name] = "clean_receipt_execute_finalize.go"
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
