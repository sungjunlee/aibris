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
		"cleanupUnitSize",
		"cleanupUnitSource",
		"cleanupUnitHardLockReasons",
	}
	eligibilityNames := []string{
		"worktreeScanStatusBlocksCleanup",
		"cleanupUnitHasReviewOnlyStatus",
	}
	identityNames := []string{
		"HasGitWorktreeMetadata",
		"resolveRepositoryIdentity",
		"readSingleGitMetadataPath",
		"canonicalGitDirectory",
		"displayRepositoryName",
	}
	uniquenessNames := []string{
		"InspectCleanupUnitsUniqueness",
		"InspectRecommendedCandidateUniqueness",
		"inspectCleanupUnitUniqueness",
		"cleanupUnitNeedsUniquenessProbe",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames)+len(eligibilityNames)+len(identityNames)+len(uniquenessNames))
	for _, name := range helperNames {
		wanted[name] = "units_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "units.go"
	}
	for _, name := range eligibilityNames {
		wanted[name] = "eligibility.go"
	}
	for _, name := range identityNames {
		wanted[name] = "identity.go"
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
		worktreeScanStatusBlocksCleanup,
		cleanupUnitHasReviewOnlyStatus,
		resolveRepositoryIdentity,
		readSingleGitMetadataPath,
		canonicalGitDirectory,
		displayRepositoryName,
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
		_ func(types.WorktreeStatus) bool                                          = worktreeScanStatusBlocksCleanup
		_ func([]types.DebrisInfo) bool                                            = cleanupUnitHasReviewOnlyStatus
		_ func(string) (string, string, error)                                     = resolveRepositoryIdentity
		_ func(string, string) (string, error)                                     = readSingleGitMetadataPath
		_ func(string) (string, error)                                             = canonicalGitDirectory
		_ func(string) string                                                      = displayRepositoryName
	)

	unitsSource := readWorktreeSource(t, "units.go")
	if !strings.Contains(unitsSource, "func BuildWorktreeCleanupUnits(") {
		t.Error("BuildWorktreeCleanupUnits is not defined in units.go")
	}
	if !strings.Contains(unitsSource, "cleanupUnitHasReviewOnlyStatus(") {
		t.Error("units.go no longer delegates eligibility to cleanupUnitHasReviewOnlyStatus")
	}
	if !strings.Contains(unitsSource, "resolveRepositoryIdentity(") {
		t.Error("units.go no longer delegates identity to resolveRepositoryIdentity")
	}
	for _, name := range []string{
		"InspectCleanupUnitsUniqueness",
		"InspectRecommendedCandidateUniqueness",
		"inspectCleanupUnitUniqueness",
		"cleanupUnitNeedsUniquenessProbe",
		"worktreeScanStatusBlocksCleanup",
		"cleanupUnitHasReviewOnlyStatus",
		"HasGitWorktreeMetadata",
		"resolveRepositoryIdentity",
		"readSingleGitMetadataPath",
		"canonicalGitDirectory",
		"displayRepositoryName",
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

	eligibilitySource := readWorktreeSource(t, "eligibility.go")
	for _, name := range []string{
		"worktreeScanStatusBlocksCleanup",
		"cleanupUnitHasReviewOnlyStatus",
	} {
		if !strings.Contains(eligibilitySource, "func "+name+"(") {
			t.Errorf("%s is not defined in eligibility.go", name)
		}
	}

	identitySource := readWorktreeSource(t, "identity.go")
	for _, name := range []string{
		"HasGitWorktreeMetadata",
		"resolveRepositoryIdentity",
		"readSingleGitMetadataPath",
		"canonicalGitDirectory",
		"displayRepositoryName",
	} {
		if !strings.Contains(identitySource, "func "+name+"(") {
			t.Errorf("%s is not defined in identity.go", name)
		}
	}
}

func TestWorktreeEligibilityDoesNotOpenGitdirFiles(t *testing.T) {
	file := parseWorktreeFile(t, "eligibility.go")
	// Allowlist must not include filesystem packages; eligibility must not open gitdir files.
	allowedImports := map[string]bool{
		`"github.com/sungjunlee/aibris/internal/types"`: true,
	}
	for _, spec := range file.Imports {
		if !allowedImports[spec.Path.Value] {
			t.Errorf("eligibility.go imports %s; want only internal/types (no filesystem packages)", spec.Path.Value)
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		switch ident.Name {
		case "HasGitWorktreeMetadata", "resolveRepositoryIdentity", "readSingleGitMetadataPath", "canonicalGitDirectory", "displayRepositoryName":
			t.Errorf("eligibility.go uses %s; eligibility must not open gitdir files", ident.Name)
		}
		return true
	})
}

func TestWorktreeIdentityDoesNotDecideCleanupEligibility(t *testing.T) {
	file := parseWorktreeFile(t, "identity.go")
	for _, spec := range file.Imports {
		if spec.Path.Value == `"github.com/sungjunlee/aibris/internal/types"` {
			t.Error("identity.go imports types; cleanup eligibility leaked into identity")
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		switch ident.Name {
		case "worktreeScanStatusBlocksCleanup", "cleanupUnitHasReviewOnlyStatus", "WorktreeActive", "WorktreeOrphaned", "WorktreePlain", "WorktreeStatus":
			t.Errorf("identity.go uses %s; identity must not decide cleanup eligibility", ident.Name)
		}
		return true
	})
}

func parseWorktreeFile(t *testing.T, path string) *ast.File {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}
