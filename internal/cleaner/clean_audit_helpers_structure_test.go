package cleaner

import (
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
// in package cleaner after the same-package extract.
var (
	_ = NewAuditTargetSet
	_ = AuditItemKey
	_ = AuditReasonsFromEligibility
	_ = AuditBlockReason
	_ = AuditReasonText
	_ = AuditReasonForOverlapSafety
	_ = MergeAuditProtections
	_ = AgeDisplay
	_ = IsReviewOnlyWorktree
	_ = ReviewOnlyWorktreeStats
	_ = LogicalInputsForAudit
	_ = BuildCleanAudit
	_ = auditComponentsForTargets
)

func TestCleanAuditHelpersLiveApartFromAuditPlanEntry(t *testing.T) {
	helperNames := []string{
		"NewAuditTargetSet",
		"Consume",
		"ExclusionReason",
		"AuditItemKey",
		"AuditReasonsFromEligibility",
		"AuditBlockReason",
		"AuditReasonText",
		"AuditReasonForOverlapSafety",
		"MergeAuditProtections",
		"AgeDisplay",
		"IsReviewOnlyWorktree",
		"ReviewOnlyWorktreeStats",
	}
	entryNames := []string{
		"BuildCleanAudit",
		"LogicalInputsForAudit",
		"auditComponentsForTargets",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "clean_audit_helpers.go"
	}
	for _, name := range entryNames {
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

func TestCleanAuditHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cleaner callers still resolve to the helper implementations.
	var (
		_ func([]types.DebrisInfo) *AuditTargetSet                                                                                                                = NewAuditTargetSet
		_ func(*AuditTargetSet, types.DebrisInfo) bool                                                                                                            = (*AuditTargetSet).Consume
		_ func(*AuditTargetSet, types.DebrisInfo) CleanAuditReason                                                                                                = (*AuditTargetSet).ExclusionReason
		_ func(types.DebrisInfo) string                                                                                                                           = AuditItemKey
		_ func(map[string]EligibilityReason) map[string]CleanAuditReason                                                                                          = AuditReasonsFromEligibility
		_ func(types.DebrisInfo, types.PruneOptions, time.Time, *AuditTargetSet, map[string]CleanAuditReason) CleanAuditReason                                    = AuditBlockReason
		_ func(CleanAuditReason, types.PruneOptions) string                                                                                                       = AuditReasonText
		_ func(OverlapSafetyReason) CleanAuditReason                                                                                                              = AuditReasonForOverlapSafety
		_ func(...map[string]CleanAuditReason) map[string]CleanAuditReason                                                                                        = MergeAuditProtections
		_ func(time.Duration) string                                                                                                                              = AgeDisplay
		_ func(types.DebrisInfo) bool                                                                                                                             = IsReviewOnlyWorktree
		_ func([]types.DebrisInfo) (int, int64)                                                                                                                   = ReviewOnlyWorktreeStats
		_ func([]types.DebrisInfo, types.PruneOptions, map[string]CleanAuditReason, time.Time) []CleanupOverlapLogicalInput                                       = LogicalInputsForAudit
		_ func([]types.DebrisInfo, []types.DebrisInfo, types.PruneOptions, int, ScanSource, map[string]CleanAuditReason, []CleanupOverlapLogicalInput) CleanAudit = BuildCleanAudit
		_ func([]types.DebrisInfo, []types.DebrisInfo, types.PruneOptions, map[string]CleanAuditReason, []CleanupOverlapLogicalInput) []CleanupOverlapComponent   = auditComponentsForTargets
	)

	auditSource := readCleanerSource(t, "clean_audit.go")
	helperSource := readCleanerSource(t, "clean_audit_helpers.go")
	for _, name := range []string{
		"BuildCleanAudit",
		"LogicalInputsForAudit",
		"auditComponentsForTargets",
	} {
		if !strings.Contains(auditSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_audit.go", name)
		}
		if strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s moved out of the public audit-plan entry", name)
		}
	}
	for _, name := range []string{
		"NewAuditTargetSet",
		"AuditItemKey",
		"AuditReasonsFromEligibility",
		"AuditBlockReason",
		"AuditReasonText",
		"AuditReasonForOverlapSafety",
		"MergeAuditProtections",
		"AgeDisplay",
		"IsReviewOnlyWorktree",
		"ReviewOnlyWorktreeStats",
	} {
		if strings.Contains(auditSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_audit.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_audit_helpers.go", name)
		}
	}
}
