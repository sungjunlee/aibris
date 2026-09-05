package cmd

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = newGuidedCleanStateFromCleanupPlan
	_ = guidedCleanupUnitItem
	_ = guidedCleanupDecisionReason
	_ = guidedMemberReason
	_ = applyGuidedPolicyReasons
	_ = selectedGuidedCleanTargets
	_ = guidedCleanAgePresets
	_ = cloneGuidedCleanStateForReplan
	_ = applyReplannedGuidedCleanup
	_ = applyReplannedGuidedRow
	_ = guidedCleanSelectionOverrides
	_ = applyGuidedCleanSelectionOverrides
	_ = guidedAgeString
	_ = buildGuidedCleanState
	_ = planGuidedCleanState
	_ = toggleGuidedCleanRow
	_ = applyGuidedCleanCommand
	_ = adjustGuidedCleanAge
	_ = replanGuidedCleanAge
)

func TestGuidedCleanHelpersLiveApartFromGuidedCleanCommandEntry(t *testing.T) {
	helperNames := []string{
		"newGuidedCleanStateFromCleanupPlan",
		"guidedCleanupUnitItem",
		"guidedCleanupDecisionReason",
		"guidedMemberReason",
		"applyGuidedPolicyReasons",
		"selectedGuidedCleanTargets",
		"guidedCleanAgePresets",
		"cloneGuidedCleanStateForReplan",
		"applyReplannedGuidedCleanup",
		"applyReplannedGuidedRow",
		"guidedCleanSelectionOverrides",
		"applyGuidedCleanSelectionOverrides",
		"guidedAgeString",
	}
	entryNames := []string{
		"buildGuidedCleanState",
		"planGuidedCleanState",
		"toggleGuidedCleanRow",
		"applyGuidedCleanCommand",
		"adjustGuidedCleanAge",
		"replanGuidedCleanAge",
	}
	entryTypes := []string{
		"guidedCleanRow",
		"guidedCleanState",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames)+len(entryTypes))
	for _, name := range helperNames {
		wanted[name] = "guided_clean_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "guided_clean.go"
	}
	for _, name := range entryTypes {
		wanted[name] = "guided_clean.go"
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

func TestGuidedCleanHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func(scanSource, string, codexActivityIndex, CleanupPolicy, []WorktreeCleanupUnit, []types.DebrisInfo, CleanupPlan) guidedCleanState = newGuidedCleanStateFromCleanupPlan
		_ func(WorktreeCleanupUnit, []types.DebrisInfo) types.DebrisInfo                                                                       = guidedCleanupUnitItem
		_ func(WorktreeCleanupDecision) string                                                                                                 = guidedCleanupDecisionReason
		_ func(WorktreeCleanupUnit, GitWorktreeMember, string) string                                                                          = guidedMemberReason
		_ func([]cleanupOverlapLogicalInput, guidedCleanState) []cleanupOverlapLogicalInput                                                    = applyGuidedPolicyReasons
		_ func(guidedCleanState) []types.DebrisInfo                                                                                            = selectedGuidedCleanTargets
		_ func(time.Duration) []time.Duration                                                                                                  = guidedCleanAgePresets
		_ func(guidedCleanState) guidedCleanState                                                                                              = cloneGuidedCleanStateForReplan
		_ func(*guidedCleanState)                                                                                                              = applyReplannedGuidedCleanup
		_ func(*guidedCleanRow, map[string]WorktreeCleanupDecision)                                                                            = applyReplannedGuidedRow
		_ func(guidedCleanState) map[string]bool                                                                                               = guidedCleanSelectionOverrides
		_ func(*guidedCleanState, map[string]bool)                                                                                             = applyGuidedCleanSelectionOverrides
		_ func(time.Duration) string                                                                                                           = guidedAgeString
		_ func(context.Context, *types.ScanResult, scanSource, time.Duration, string) (guidedCleanState, error)                                = buildGuidedCleanState
		_ func(guidedCleanState, string) (guidedCleanState, string, bool)                                                                      = applyGuidedCleanCommand
		_ func(guidedCleanState, time.Duration) (guidedCleanState, string)                                                                     = replanGuidedCleanAge
	)

	guidedSource := readCmdSource(t, "guided_clean.go")
	for _, name := range []string{
		"buildGuidedCleanState",
		"planGuidedCleanState",
		"toggleGuidedCleanRow",
		"applyGuidedCleanCommand",
		"adjustGuidedCleanAge",
		"replanGuidedCleanAge",
	} {
		if !strings.Contains(guidedSource, "func "+name+"(") {
			t.Errorf("%s is not defined in guided_clean.go", name)
		}
	}
	for _, name := range []string{
		"newGuidedCleanStateFromCleanupPlan",
		"guidedCleanAgePresets",
		"guidedCleanSelectionOverrides",
		"cloneGuidedCleanStateForReplan",
		"applyReplannedGuidedCleanup",
		"applyGuidedCleanSelectionOverrides",
		"guidedAgeString",
	} {
		if strings.Contains(guidedSource, "func "+name+"(") {
			t.Errorf("%s is still defined in guided_clean.go", name)
		}
		if !strings.Contains(guidedSource, name+"(") {
			t.Errorf("guided_clean.go no longer delegates to %s", name)
		}
	}
	for _, name := range []string{
		"guidedCleanupUnitItem",
		"guidedCleanupDecisionReason",
		"guidedMemberReason",
		"applyGuidedPolicyReasons",
		"selectedGuidedCleanTargets",
		"applyReplannedGuidedRow",
	} {
		if strings.Contains(guidedSource, "func "+name+"(") {
			t.Errorf("%s is still defined in guided_clean.go", name)
		}
	}
}
