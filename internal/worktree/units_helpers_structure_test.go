package worktree

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestWorktreeMemberDiscoveryHelpersLiveApartFromCleanupUnitFacade(t *testing.T) {
	helperNames := []string{
		"discoverGitWorktreeMembers",
		"classifyMissingCleanupMember",
		"twoLevelGitWorktreePaths",
		"ownerGitMarkerState",
	}
	facadeNames := []string{
		"BuildWorktreeCleanupUnits",
		"worktreeScanStatusBlocksCleanup",
		"cleanupUnitHasReviewOnlyStatus",
		"cleanupUnitSize",
		"cleanupUnitSource",
		"cleanupUnitHardLockReasons",
	}
	uniquenessNames := []string{
		"InspectCleanupUnitsUniqueness",
		"InspectRecommendedCandidateUniqueness",
		"inspectCleanupUnitUniqueness",
		"cleanupUnitNeedsUniquenessProbe",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames)+len(uniquenessNames))
	for _, name := range helperNames {
		wanted[name] = "units_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "units.go"
	}
	for _, name := range uniquenessNames {
		wanted[name] = "units_uniqueness.go"
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

func TestWorktreeUnitsHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper symbols stay the package-level identities
	// that BuildWorktreeCleanupUnits already calls. Public names remain
	// assignable from this package so existing imports keep working.
	helpers := []any{
		discoverGitWorktreeMembers,
		classifyMissingCleanupMember,
		twoLevelGitWorktreePaths,
		ownerGitMarkerState,
		inspectCleanupUnitUniqueness,
		cleanupUnitNeedsUniquenessProbe,
	}
	public := []any{
		BuildWorktreeCleanupUnits,
		HasGitWorktreeMetadata,
		BuildGitWorktreeMember,
		InspectCleanupUnitsUniqueness,
		InspectRecommendedCandidateUniqueness,
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
		_ func(context.Context, []types.DebrisInfo) ([]WorktreeCleanupUnit, error) = BuildWorktreeCleanupUnits
		_ func(string) bool                                                        = HasGitWorktreeMetadata
		_ func(context.Context, string) GitWorktreeMember                          = BuildGitWorktreeMember
		_ func(context.Context, []WorktreeCleanupUnit)                             = InspectCleanupUnitsUniqueness
		_ func(context.Context, []WorktreeCleanupUnit, CleanupPolicy)              = InspectRecommendedCandidateUniqueness
		_ func(context.Context, *WorktreeCleanupUnit)                              = inspectCleanupUnitUniqueness
		_ func(WorktreeCleanupUnit, CleanupPolicy, map[string]bool) bool           = cleanupUnitNeedsUniquenessProbe
	)

	unitsSource := readWorktreeSource(t, "units.go")
	if !strings.Contains(unitsSource, "func BuildWorktreeCleanupUnits(") {
		t.Error("BuildWorktreeCleanupUnits is not defined in units.go")
	}
	for _, name := range []string{
		"InspectCleanupUnitsUniqueness",
		"InspectRecommendedCandidateUniqueness",
		"inspectCleanupUnitUniqueness",
		"cleanupUnitNeedsUniquenessProbe",
	} {
		if strings.Contains(unitsSource, "func "+name+"(") {
			t.Errorf("%s is still defined in units.go", name)
		}
	}

	uniquenessSource := readWorktreeSource(t, "units_uniqueness.go")
	for _, name := range []string{
		"InspectCleanupUnitsUniqueness",
		"InspectRecommendedCandidateUniqueness",
		"inspectCleanupUnitUniqueness",
		"cleanupUnitNeedsUniquenessProbe",
	} {
		if !strings.Contains(uniquenessSource, "func "+name+"(") {
			t.Errorf("%s is not defined in units_uniqueness.go", name)
		}
	}
}
