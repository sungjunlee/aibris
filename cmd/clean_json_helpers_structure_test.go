package cmd

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = failCleanJSON
	_ = buildCleanJSONPlan
	_ = renderCleanJSONPlanDocument
	_ = encodeCleanJSON
	_ = cleanJSONPlanCandidates
	_ = buildCleanJSONSnapshotComponents
	_ = cleanJSONPolicyForAuditItem
	_ = uniqueCleanJSONReasonCodes
	_ = cleanJSONRowIdentityKey
	_ = cleanJSONInput
	_ = cleanJSONSource
	_ = cleanJSONGuidedPolicy
	_ = cleanJSONEvidenceFromPlan
	_ = cleanJSONUnifiedPlan
	_ = cleanJSONAuditComponents
	_ = cleanJSONProtections
	_ = cleanJSONSchemaVersion
	_ = cleanJSONDecisionSelected
	_ = cleanJSONDecisionReviewable
	_ = cleanJSONDecisionProtected
	_ = cleanJSONDecisionSkipped
	_ = cleanJSONPolicyEligible
	_ = cleanJSONPolicyRecommended
	_ = cleanJSONPolicyReviewable
	_ = cleanJSONPolicyProtected
	_ = cleanJSONPolicySkipped
	_ = runCleanJSON
)

func TestCleanJSONHelpersLiveApartFromCleanJSONCommandEntry(t *testing.T) {
	helperNames := []string{
		"failCleanJSON",
		"buildCleanJSONPlan",
		"renderCleanJSONPlanDocument",
		"encodeCleanJSON",
		"cleanJSONPlanCandidates",
		"buildCleanJSONSnapshotComponents",
		"cleanJSONPolicyForAuditItem",
		"uniqueCleanJSONReasonCodes",
		"cleanJSONRowIdentityKey",
		"cleanJSONInput",
		"cleanJSONSource",
		"cleanJSONGuidedPolicy",
		"cleanJSONEvidenceFromPlan",
		"cleanJSONUnifiedPlan",
		"cleanJSONAuditComponents",
		"cleanJSONProtections",
		"cleanJSONSchemaVersion",
		"cleanJSONDecisionSelected",
		"cleanJSONDecisionReviewable",
		"cleanJSONDecisionProtected",
		"cleanJSONDecisionSkipped",
		"cleanJSONPolicyEligible",
		"cleanJSONPolicyRecommended",
		"cleanJSONPolicyReviewable",
		"cleanJSONPolicyProtected",
		"cleanJSONPolicySkipped",
		"cleanJSONPlan",
		"cleanJSONPhysicalTarget",
		"cleanJSONRow",
		"cleanJSONSnapshotComponent",
	}
	entryNames := []string{
		"runCleanJSON",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "clean_json_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "clean_json.go"
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
						switch s := spec.(type) {
						case *ast.ValueSpec:
							for _, ident := range s.Names {
								if _, ok := wanted[ident.Name]; !ok {
									continue
								}
								owners[ident.Name] = append(owners[ident.Name], base)
							}
						case *ast.TypeSpec:
							if _, ok := wanted[s.Name.Name]; !ok {
								continue
							}
							owners[s.Name.Name] = append(owners[s.Name.Name], base)
						}
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

func TestCleanJSONHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func(string)                                                                                                                                                                    = failCleanJSON
		_ func(context.Context, *types.ScanResult, scanSource, types.PruneOptions, *guidedCleanState, []types.DebrisInfo, map[string]cleanAuditReason, cleanAudit) (cleanJSONPlan, error) = buildCleanJSONPlan
		_ func(scanSource, types.PruneOptions, *guidedCleanState, CleanupPlanEvidence, []cleanJSONSnapshotComponent) cleanJSONPlan                                                        = renderCleanJSONPlanDocument
		_ func(io.Writer, cleanJSONPlan) error                                                                                                                                            = encodeCleanJSON
		_ func(*guidedCleanState, []types.DebrisInfo, types.PruneOptions) []CleanupPlanCandidate                                                                                          = cleanJSONPlanCandidates
		_ func(UnifiedCleanupPlan, []cleanupOverlapComponent, []types.DebrisInfo, map[string]cleanAuditReason) []cleanJSONSnapshotComponent                                               = buildCleanJSONSnapshotComponents
		_ func(types.DebrisInfo, types.PruneOptions, map[string]cleanAuditReason, time.Time) (string, []string)                                                                           = cleanJSONPolicyForAuditItem
		_ func([]string) []string                                                                                                                                                         = uniqueCleanJSONReasonCodes
		_ func(types.DebrisInfo) string                                                                                                                                                   = cleanJSONRowIdentityKey
	)

	cleanJSONSourceFile := readCmdSource(t, "clean_json.go")
	if !strings.Contains(cleanJSONSourceFile, "func runCleanJSON(") {
		t.Error("runCleanJSON is not defined in clean_json.go")
	}
	for _, name := range []string{
		"failCleanJSON",
		"buildCleanJSONPlan",
		"encodeCleanJSON",
		"buildCleanJSONSnapshotComponents",
	} {
		if strings.Contains(cleanJSONSourceFile, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_json.go", name)
		}
		if !strings.Contains(cleanJSONSourceFile, name+"(") {
			t.Errorf("clean_json.go no longer delegates to %s", name)
		}
	}
	for _, name := range []string{
		"renderCleanJSONPlanDocument",
		"cleanJSONPlanCandidates",
		"cleanJSONPolicyForAuditItem",
		"uniqueCleanJSONReasonCodes",
		"cleanJSONRowIdentityKey",
		"cleanJSONInput",
		"cleanJSONSource",
		"cleanJSONGuidedPolicy",
		"cleanJSONEvidenceFromPlan",
		"cleanJSONUnifiedPlan",
		"cleanJSONAuditComponents",
		"cleanJSONProtections",
	} {
		if strings.Contains(cleanJSONSourceFile, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_json.go", name)
		}
	}
}
