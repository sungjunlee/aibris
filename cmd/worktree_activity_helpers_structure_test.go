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
	"github.com/sungjunlee/aibris/internal/worktree"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = collectMemberActivity
	_ = worktreeActivityAvailability
	_ = worktreeActivityTool
	_ = codexActivityAvailability
	_ = headReflogActivity
	_ = cleanupUnitActivityRows
	_ = memberFallbackActivity
	_ = memberCodexIdentity
	_ = BuildWorktreeCleanupUnitsWithActivity
	_ = enrichWorktreeCleanupActivity
)

func TestWorktreeActivityHelpersLiveApartFromActivityEntry(t *testing.T) {
	helperNames := []string{
		"collectMemberActivity",
		"worktreeActivityAvailability",
		"worktreeActivityTool",
		"codexActivityAvailability",
		"headReflogActivity",
		"cleanupUnitActivityRows",
		"memberFallbackActivity",
		"memberCodexIdentity",
	}
	entryNames := []string{
		"BuildWorktreeCleanupUnitsWithActivity",
		"enrichWorktreeCleanupActivity",
	}
	helperTypes := []string{
		"codexActivityIdentity",
	}
	entryTypes := []string{
		"worktreeActivityOptions",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames)+len(helperTypes)+len(entryTypes))
	for _, name := range helperNames {
		wanted[name] = "worktree_activity_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "worktree_activity.go"
	}
	for _, name := range helperTypes {
		wanted[name] = "worktree_activity_helpers.go"
	}
	for _, name := range entryTypes {
		wanted[name] = "worktree_activity.go"
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
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
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

func TestWorktreeActivityHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func(context.Context, *GitWorktreeMember, time.Time, codexActivityIdentity, types.Tool, string, codexActivityIndex, worktree.GitCommandRunner) error = collectMemberActivity
		_ func(types.Tool, string, codexActivityIndex) (bool, string, string)                                                                                  = worktreeActivityAvailability
		_ func([]types.DebrisInfo, string) types.Tool                                                                                                          = worktreeActivityTool
		_ func(string, codexActivityIndex) (bool, string, string)                                                                                              = codexActivityAvailability
		_ func(context.Context, string, worktree.GitCommandRunner) (WorktreeActivityEvidence, error)                                                           = headReflogActivity
		_ func([]types.DebrisInfo) map[string][]types.DebrisInfo                                                                                               = cleanupUnitActivityRows
		_ func(string, string, []types.DebrisInfo) time.Time                                                                                                   = memberFallbackActivity
		_ func(string, []types.DebrisInfo) codexActivityIdentity                                                                                               = memberCodexIdentity
		_ func(context.Context, []types.DebrisInfo) ([]WorktreeCleanupUnit, error)                                                                             = BuildWorktreeCleanupUnitsWithActivity
		_ func(context.Context, []WorktreeCleanupUnit, []types.DebrisInfo, worktreeActivityOptions) error                                                      = enrichWorktreeCleanupActivity
	)

	activitySource := readCmdSource(t, "worktree_activity.go")
	helperSource := readCmdSource(t, "worktree_activity_helpers.go")
	for _, name := range []string{
		"BuildWorktreeCleanupUnitsWithActivity",
		"enrichWorktreeCleanupActivity",
	} {
		if !strings.Contains(activitySource, "func "+name+"(") {
			t.Errorf("%s is not defined in worktree_activity.go", name)
		}
		if strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s moved out of the public activity entry", name)
		}
	}
	for _, name := range []string{
		"collectMemberActivity",
		"worktreeActivityAvailability",
		"worktreeActivityTool",
		"cleanupUnitActivityRows",
		"memberFallbackActivity",
		"memberCodexIdentity",
	} {
		if strings.Contains(activitySource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_activity.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in worktree_activity_helpers.go", name)
		}
		if !strings.Contains(activitySource, name+"(") {
			t.Errorf("worktree_activity.go no longer delegates to %s", name)
		}
	}
	for _, name := range []string{
		"codexActivityAvailability",
		"headReflogActivity",
	} {
		if strings.Contains(activitySource, "func "+name+"(") {
			t.Errorf("%s is still defined in worktree_activity.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in worktree_activity_helpers.go", name)
		}
	}
}
