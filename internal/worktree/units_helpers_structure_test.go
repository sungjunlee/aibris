package worktree

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
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

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "units_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "units.go"
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
	)
}
