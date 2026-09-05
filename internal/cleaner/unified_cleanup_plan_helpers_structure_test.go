package cleaner

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestUnifiedCleanupPlanHelpersLiveApartFromPlanEntry(t *testing.T) {
	helperNames := []string{
		"buildCleanupPhysicalComponents",
		"aggregateCleanupPlanComponentSelection",
		"cleanupPhysicalTargetByKey",
		"cleanupPlanOwnerRowKey",
		"cleanupPlanRowContainsLockedTarget",
		"appendUniqueCleanupPlanReason",
	}
	facadeNames := []string{
		"BuildUnifiedCleanupPlan",
		"ValidateForExecution",
		"SelectedPhysicalTargets",
		"Totals",
		"validCleanupPlanSelection",
		"aggregateCleanupPlanSelection",
		"cleanupPlanRepresentative",
		"cleanupPlanCandidateStableKey",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "unified_cleanup_plan_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "unified_cleanup_plan.go"
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

func TestUnifiedCleanupPlanHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper symbols stay the package-level identities
	// that BuildUnifiedCleanupPlan already calls. Public names remain
	// assignable from this package so existing imports keep working.
	helpers := []any{
		buildCleanupPhysicalComponents,
		aggregateCleanupPlanComponentSelection,
		cleanupPhysicalTargetByKey,
		cleanupPlanOwnerRowKey,
		cleanupPlanRowContainsLockedTarget,
		appendUniqueCleanupPlanReason,
	}
	public := []any{
		BuildUnifiedCleanupPlan,
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
		_ func(context.Context, []CleanupPlanCandidate, CleanupPlanEvidence) (UnifiedCleanupPlan, error) = BuildUnifiedCleanupPlan
		_ func(context.Context, time.Time) error                                                         = (UnifiedCleanupPlan{}).ValidateForExecution
		_ func() []types.DebrisInfo                                                                      = (UnifiedCleanupPlan{}).SelectedPhysicalTargets
		_ func() CleanupPlanTotals                                                                       = (UnifiedCleanupPlan{}).Totals
	)
}
