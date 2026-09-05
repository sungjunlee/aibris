package cleaner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestPhysicalCleanAuditLivesApartFromAuditTypes(t *testing.T) {
	physicalNames := []string{
		"BuildPhysicalCleanAudit",
		"BuildPhysicalCleanAuditWithLogicalInputs",
		"cleanAuditCategoryFor",
		"addCleanAuditReasonStat",
		"AuditPhysicalComponents",
		"auditPhysicalComponentsWithLogicalInputs",
		"cleanupLogicalItems",
		"auditComponentReason",
		"auditLogicalReason",
		"auditMainReason",
		"mixedWorktreeSkipReason",
	}
	auditNames := []string{
		"BuildCleanAudit",
		"LogicalInputsForAudit",
		"auditComponentsForTargets",
	}

	wanted := make(map[string]string, len(physicalNames)+len(auditNames))
	for _, name := range physicalNames {
		wanted[name] = "clean_audit_physical.go"
	}
	for _, name := range auditNames {
		wanted[name] = "clean_audit.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cleaner: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
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
