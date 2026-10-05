package worktree

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Boundary invariants: eligibility decides from scan facts only, and identity
// resolution never decides eligibility.

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
