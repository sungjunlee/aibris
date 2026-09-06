package cleaner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cleaner after the same-package extract.
var (
	_ = AuditPhysicalComponents
	_ = auditPhysicalComponentsWithLogicalInputs
	_ = cleanupLogicalItems
	_ = BuildPhysicalCleanAudit
	_ = BuildPhysicalCleanAuditWithLogicalInputs
)

func TestPhysicalCleanAuditHelpersLiveApartFromPhysicalEntry(t *testing.T) {
	helperNames := []string{
		"AuditPhysicalComponents",
		"auditPhysicalComponentsWithLogicalInputs",
		"cleanupLogicalItems",
	}
	entryNames := []string{
		"BuildPhysicalCleanAudit",
		"BuildPhysicalCleanAuditWithLogicalInputs",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "clean_audit_physical_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "clean_audit_physical.go"
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
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
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

func TestPhysicalCleanAuditHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cleaner callers still resolve to the helper implementations.
	helpers := []any{
		AuditPhysicalComponents,
		auditPhysicalComponentsWithLogicalInputs,
		cleanupLogicalItems,
	}
	public := []any{
		BuildPhysicalCleanAudit,
		BuildPhysicalCleanAuditWithLogicalInputs,
		AuditPhysicalComponents,
	}
	for i, fn := range helpers {
		if fn == nil {
			t.Errorf("helper %d is nil", i)
		}
	}
	for i, fn := range public {
		if fn == nil {
			t.Errorf("public %d is nil", i)
		}
	}

	var (
		_ func([]types.DebrisInfo, []CleanupOverlapComponent) ([]CleanupOverlapComponent, map[int]bool)                               = AuditPhysicalComponents
		_ func([]types.DebrisInfo, []CleanupOverlapComponent, []CleanupOverlapLogicalInput) ([]CleanupOverlapComponent, map[int]bool) = auditPhysicalComponentsWithLogicalInputs
		_ func([]CleanupOverlapLogicalInput) []types.DebrisInfo                                                                       = cleanupLogicalItems
	)

	physicalSource := readCleanerSource(t, "clean_audit_physical.go")
	helperSource := readCleanerSource(t, "clean_audit_physical_helpers.go")
	for _, name := range []string{
		"BuildPhysicalCleanAudit",
		"BuildPhysicalCleanAuditWithLogicalInputs",
	} {
		if !strings.Contains(physicalSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_audit_physical.go", name)
		}
		if strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s moved out of the public physical-audit entry", name)
		}
	}
	if !strings.Contains(physicalSource, "auditPhysicalComponentsWithLogicalInputs(") {
		t.Error("clean_audit_physical.go no longer delegates to auditPhysicalComponentsWithLogicalInputs")
	}
	for _, name := range []string{
		"AuditPhysicalComponents",
		"auditPhysicalComponentsWithLogicalInputs",
		"cleanupLogicalItems",
	} {
		if strings.Contains(physicalSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_audit_physical.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_audit_physical_helpers.go", name)
		}
	}
}
