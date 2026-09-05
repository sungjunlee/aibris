package worktree

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Compile-time re-export identity: the original helper names still resolve
// in package worktree after the same-package extract.
var (
	_ = cleanupUnitHardLockReasonCodes
	_ = cleanupUnitHasRegisteredActivitySource
	_ = cleanupUnitContainsPath
	_ = PlanWorktreeCleanup
	_ = DefaultCleanupPolicy
	_ = FillCleanupPolicy
	_ = CleanupUnitStableKey
	_ = DecisionReasonDescription
)

func TestWorktreePolicyHelpersLiveApartFromPolicyEntry(t *testing.T) {
	helperNames := []string{
		"cleanupUnitHardLockReasonCodes",
		"cleanupUnitHasRegisteredActivitySource",
		"cleanupUnitContainsPath",
	}
	facadeNames := []string{
		"PlanWorktreeCleanup",
		"DefaultCleanupPolicy",
		"FillCleanupPolicy",
		"classifyCleanupUnit",
		"CleanupUnitStableKey",
		"DecisionReasonDescription",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "policy_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "policy.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse worktree: %v", err)
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

func TestWorktreePolicyHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing worktree callers still resolve to the helper implementations.
	helpers := []any{
		cleanupUnitHardLockReasonCodes,
		cleanupUnitHasRegisteredActivitySource,
		cleanupUnitContainsPath,
	}
	public := []any{
		PlanWorktreeCleanup,
		DefaultCleanupPolicy,
		FillCleanupPolicy,
		CleanupUnitStableKey,
		DecisionReasonDescription,
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
		_ func([]WorktreeCleanupUnit, CleanupPolicy) CleanupPlan        = PlanWorktreeCleanup
		_ func(time.Time) CleanupPolicy                                 = DefaultCleanupPolicy
		_ func(CleanupPolicy) CleanupPolicy                             = FillCleanupPolicy
		_ func(WorktreeCleanupUnit, CleanupPolicy) []DecisionReasonCode = cleanupUnitHardLockReasonCodes
		_ func(WorktreeCleanupUnit) bool                                = cleanupUnitHasRegisteredActivitySource
		_ func(string, string) bool                                     = cleanupUnitContainsPath
		_ func(WorktreeCleanupUnit) string                              = CleanupUnitStableKey
		_ func(DecisionReasonCode) string                               = DecisionReasonDescription
	)

	policySource := readWorktreeSource(t, "policy.go")
	if !strings.Contains(policySource, "func PlanWorktreeCleanup(") {
		t.Error("PlanWorktreeCleanup is not defined in policy.go")
	}
	for _, name := range []string{
		"cleanupUnitHardLockReasonCodes",
		"cleanupUnitHasRegisteredActivitySource",
		"cleanupUnitContainsPath",
	} {
		if strings.Contains(policySource, "func "+name+"(") {
			t.Errorf("%s is still defined in policy.go", name)
		}
	}
	if !strings.Contains(policySource, "cleanupUnitHardLockReasonCodes(") {
		t.Error("policy.go no longer delegates to cleanupUnitHardLockReasonCodes")
	}
	if !strings.Contains(policySource, "cleanupUnitHasRegisteredActivitySource(") {
		t.Error("policy.go no longer delegates to cleanupUnitHasRegisteredActivitySource")
	}
}

func readWorktreeSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
