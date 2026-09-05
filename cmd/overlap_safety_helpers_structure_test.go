package cmd

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = defaultCleanupOverlapLogicalInputs
	_ = buildCleanupOverlapComponents
	_ = cleanupSafetyMatchForInput
	_ = cleanupLogicalL1Reason
	_ = cleanupLogicalRevalidationRequired
	_ = cleanupOverlapComponentForTarget
	_ = overlapSafetyAuditProtections
	_ = applyCleanupOverlapSafety
	_ = applyCleanupOverlapSafetyWithRows
	_ = newDefaultCleanupOverlapSafetyRuntime
	_ = newCleanupOverlapSafetyRuntime
)

func TestOverlapSafetyHelpersLiveApartFromOverlapSafetyCommandEntry(t *testing.T) {
	helperNames := []string{
		"defaultCleanupOverlapLogicalInputs",
		"buildCleanupOverlapComponents",
		"cleanupSafetyMatchForInput",
		"cleanupLogicalL1Reason",
		"cleanupLogicalRevalidationRequired",
		"cleanupOverlapComponentForTarget",
		"overlapSafetyAuditProtections",
	}
	entryNames := []string{
		"applyCleanupOverlapSafety",
		"applyCleanupOverlapSafetyWithRows",
		"newDefaultCleanupOverlapSafetyRuntime",
		"newCleanupOverlapSafetyRuntime",
	}
	entryTypes := []string{
		"cleanupOverlapSafetyRuntime",
		"cleanupOverlapSafetySelection",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames)+len(entryTypes))
	for _, name := range helperNames {
		wanted[name] = "overlap_safety_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "overlap_safety.go"
	}
	for _, name := range entryTypes {
		wanted[name] = "overlap_safety.go"
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

func TestOverlapSafetyHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func([]types.DebrisInfo, []types.DebrisInfo) []cleanupOverlapLogicalInput                                                                   = defaultCleanupOverlapLogicalInputs
		_ func(cleaner.OverlapSafetyPlan, []cleanupOverlapLogicalInput) []cleanupOverlapComponent                                                     = buildCleanupOverlapComponents
		_ func([]cleaner.OverlapSafetyMatch, types.DebrisInfo) (cleaner.OverlapSafetyMatch, bool)                                                     = cleanupSafetyMatchForInput
		_ func(cleaner.OverlapSafetyComponent, types.DebrisInfo, string) string                                                                       = cleanupLogicalL1Reason
		_ func(cleaner.OverlapSafetyComponent, types.DebrisInfo, string) bool                                                                         = cleanupLogicalRevalidationRequired
		_ func(cleanupOverlapSafetySelection, types.DebrisInfo) (cleanupOverlapComponent, bool)                                                       = cleanupOverlapComponentForTarget
		_ func(cleaner.OverlapSafetyPlan) map[string]cleanAuditReason                                                                                 = overlapSafetyAuditProtections
		_ func(context.Context, cleanupOverlapSafetyRuntime, []types.DebrisInfo) (cleanupOverlapSafetySelection, error)                               = applyCleanupOverlapSafety
		_ func(context.Context, cleanupOverlapSafetyRuntime, []types.DebrisInfo, []cleanupOverlapLogicalInput) (cleanupOverlapSafetySelection, error) = applyCleanupOverlapSafetyWithRows
		_ func(context.Context) (cleanupOverlapSafetyRuntime, error)                                                                                  = newDefaultCleanupOverlapSafetyRuntime
		_ func(context.Context, *scanner.Scanner, cleaner.AgentStateRevalidatorLookup) (cleanupOverlapSafetyRuntime, error)                           = newCleanupOverlapSafetyRuntime
	)

	safetySource := readCmdSource(t, "overlap_safety.go")
	if !strings.Contains(safetySource, "func applyCleanupOverlapSafety(") {
		t.Error("applyCleanupOverlapSafety is not defined in overlap_safety.go")
	}
	if !strings.Contains(safetySource, "func applyCleanupOverlapSafetyWithRows(") {
		t.Error("applyCleanupOverlapSafetyWithRows is not defined in overlap_safety.go")
	}
	if !strings.Contains(safetySource, "func newDefaultCleanupOverlapSafetyRuntime(") {
		t.Error("newDefaultCleanupOverlapSafetyRuntime is not defined in overlap_safety.go")
	}
	if !strings.Contains(safetySource, "func newCleanupOverlapSafetyRuntime(") {
		t.Error("newCleanupOverlapSafetyRuntime is not defined in overlap_safety.go")
	}
	for _, name := range []string{
		"defaultCleanupOverlapLogicalInputs",
		"buildCleanupOverlapComponents",
		"overlapSafetyAuditProtections",
	} {
		if strings.Contains(safetySource, "func "+name+"(") {
			t.Errorf("%s is still defined in overlap_safety.go", name)
		}
		if !strings.Contains(safetySource, name+"(") {
			t.Errorf("overlap_safety.go no longer delegates to %s", name)
		}
	}
	for _, name := range []string{
		"cleanupSafetyMatchForInput",
		"cleanupLogicalL1Reason",
		"cleanupLogicalRevalidationRequired",
		"cleanupOverlapComponentForTarget",
	} {
		if strings.Contains(safetySource, "func "+name+"(") {
			t.Errorf("%s is still defined in overlap_safety.go", name)
		}
	}
}
