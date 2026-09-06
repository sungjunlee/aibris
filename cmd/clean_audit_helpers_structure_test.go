package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = cleanupOverlapLogicalInputsForAudit
	_ = buildCleanAudit
	_ = buildPhysicalCleanAudit
	_ = buildPhysicalCleanAuditWithLogicalInputs
	_ = cleanAuditPhysicalComponents
	_ = newCleanAuditTargetSet
	_ = cleanAuditItemKey
	_ = cleanAuditReasonsFromEligibility
	_ = cleanAuditBlockReason
	_ = cleanAuditReasonText
	_ = cleanAuditReasonForOverlapSafety
	_ = mergeCleanAuditProtections
	_ = cleanupLogicalRelation
	_ = cleanupLogicalPolicyReason
	_ = ensureCleanupOwnerLogicalRow
	_ = sortCleanupOverlapLogicalRows
	_ = printCleanAudit
	_ = printCleanAuditSummary
	_ = printCleanAuditCategories
	_ = cleanAuditPolicyLine
	_ = cleanAuditScanSourceLine
)

func TestCleanAuditHelpersLiveApartFromAuditEntry(t *testing.T) {
	helperNames := []string{
		"cleanupOverlapLogicalInputsForAudit",
		"buildCleanAudit",
		"buildPhysicalCleanAudit",
		"buildPhysicalCleanAuditWithLogicalInputs",
		"cleanAuditPhysicalComponents",
		"newCleanAuditTargetSet",
		"cleanAuditItemKey",
		"cleanAuditReasonsFromEligibility",
		"cleanAuditBlockReason",
		"cleanAuditReasonText",
		"cleanAuditReasonForOverlapSafety",
		"mergeCleanAuditProtections",
		"cleanupLogicalRelation",
		"cleanupLogicalPolicyReason",
		"ensureCleanupOwnerLogicalRow",
		"sortCleanupOverlapLogicalRows",
	}
	entryNames := []string{
		"printCleanAudit",
		"printCleanAuditSummary",
		"printCleanAuditCategories",
		"cleanAuditPolicyLine",
		"cleanAuditScanSourceLine",
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

func TestCleanAuditHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func([]types.DebrisInfo, types.PruneOptions, map[string]cleanAuditReason) []cleanupOverlapLogicalInput                                                                             = cleanupOverlapLogicalInputsForAudit
		_ func([]types.DebrisInfo, []types.DebrisInfo, types.PruneOptions, int, scanSource, map[string]cleanAuditReason) cleanAudit                                                          = buildCleanAudit
		_ func([]types.DebrisInfo, []cleanupOverlapComponent, []types.DebrisInfo, types.PruneOptions, int, scanSource, map[string]cleanAuditReason) cleanAudit                               = buildPhysicalCleanAudit
		_ func([]types.DebrisInfo, []cleanupOverlapComponent, []types.DebrisInfo, types.PruneOptions, int, scanSource, map[string]cleanAuditReason, []cleanupOverlapLogicalInput) cleanAudit = buildPhysicalCleanAuditWithLogicalInputs
		_ func([]types.DebrisInfo, []cleanupOverlapComponent) ([]cleanupOverlapComponent, map[int]bool)                                                                                      = cleanAuditPhysicalComponents
		_ func([]types.DebrisInfo) *cleanAuditTargetSet                                                                                                                                      = newCleanAuditTargetSet
		_ func(types.DebrisInfo) string                                                                                                                                                      = cleanAuditItemKey
		_ func(map[string]cleaner.EligibilityReason) map[string]cleanAuditReason                                                                                                             = cleanAuditReasonsFromEligibility
		_ func(types.DebrisInfo, types.PruneOptions, time.Time, *cleanAuditTargetSet, map[string]cleanAuditReason) cleanAuditReason                                                          = cleanAuditBlockReason
		_ func(cleanAuditReason, types.PruneOptions) string                                                                                                                                  = cleanAuditReasonText
		_ func(cleaner.OverlapSafetyReason) cleanAuditReason                                                                                                                                 = cleanAuditReasonForOverlapSafety
		_ func(...map[string]cleanAuditReason) map[string]cleanAuditReason                                                                                                                   = mergeCleanAuditProtections
		_ func(string, string) (cleanupOverlapRelation, bool)                                                                                                                                = cleanupLogicalRelation
		_ func(cleanupOverlapLogicalInput) string                                                                                                                                            = cleanupLogicalPolicyReason
		_ func([]cleanupOverlapLogicalRow, types.DebrisInfo, string) []cleanupOverlapLogicalRow                                                                                              = ensureCleanupOwnerLogicalRow
		_ func([]cleanupOverlapLogicalRow, types.DebrisInfo)                                                                                                                                 = sortCleanupOverlapLogicalRows
		_ func(cleanAudit, types.PruneOptions)                                                                                                                                               = printCleanAudit
		_ func(types.PruneOptions) string                                                                                                                                                    = cleanAuditPolicyLine
		_ func(scanSource) string                                                                                                                                                            = cleanAuditScanSourceLine
	)

	auditSource := readCmdSource(t, "clean_audit.go")
	helperSource := readCmdSource(t, "clean_audit_helpers.go")
	for _, name := range []string{
		"printCleanAudit",
		"printCleanAuditSummary",
		"printCleanAuditCategories",
		"cleanAuditPolicyLine",
		"cleanAuditScanSourceLine",
	} {
		if !strings.Contains(auditSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_audit.go", name)
		}
		if strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s moved out of the public audit entry", name)
		}
	}
	for _, name := range []string{
		"cleanupOverlapLogicalInputsForAudit",
		"buildCleanAudit",
		"buildPhysicalCleanAudit",
		"buildPhysicalCleanAuditWithLogicalInputs",
		"cleanAuditPhysicalComponents",
		"newCleanAuditTargetSet",
		"cleanAuditItemKey",
		"cleanAuditReasonsFromEligibility",
		"cleanAuditBlockReason",
		"cleanAuditReasonText",
		"cleanAuditReasonForOverlapSafety",
		"mergeCleanAuditProtections",
		"cleanupLogicalRelation",
		"cleanupLogicalPolicyReason",
		"ensureCleanupOwnerLogicalRow",
		"sortCleanupOverlapLogicalRows",
	} {
		if strings.Contains(auditSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_audit.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_audit_helpers.go", name)
		}
	}
}
