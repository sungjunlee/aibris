package adapter

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

// Compile-time re-export identity: the original helper names still resolve
// in package adapter after the same-package extract.
var (
	_ = (*WorktreeAdapter).scanExplicitRootUnits
	_ = (*WorktreeAdapter).scanRootAsWorktreeUnit
	_ = linkedWorktreeOwnerAt
	_ = isWorktreeContainerMember
	_ = worktreeUnitMeta
	_ = (*WorktreeAdapter).scanWorktreeUnit
	_ = applyWorktreeUnitSizes
	_ = (*WorktreeAdapter).Scan
	_ = (*WorktreeAdapter).scanWorktreeRoots
	_ = NewWorktreeAdapter
)

func TestWorktreeHelpersLiveApartFromScanEntry(t *testing.T) {
	helperNames := []string{
		"scanExplicitRootUnits",
		"scanRootAsWorktreeUnit",
		"linkedWorktreeOwnerAt",
		"isWorktreeContainerMember",
		"worktreeUnitMeta",
		"scanWorktreeUnit",
		"applyWorktreeUnitSizes",
	}
	facadeNames := []string{
		"scanWorktreeRoots",
		"prepareWorktreeScan",
		"collectWorktreeContainers",
		"scanCollectedContainers",
		"registeredWorktreeContainers",
		"filterDebrisUnderRoots",
		"sortWorktreeResults",
		"NewWorktreeAdapter",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "worktree_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "worktree.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse adapter: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
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

func TestWorktreeHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing adapter callers still resolve to the helper implementations.
	helpers := []any{
		(*WorktreeAdapter).scanExplicitRootUnits,
		(*WorktreeAdapter).scanRootAsWorktreeUnit,
		linkedWorktreeOwnerAt,
		isWorktreeContainerMember,
		worktreeUnitMeta,
		(*WorktreeAdapter).scanWorktreeUnit,
		applyWorktreeUnitSizes,
	}
	public := []any{
		NewWorktreeAdapter,
		(*WorktreeAdapter).Name,
		(*WorktreeAdapter).Category,
		(*WorktreeAdapter).Scan,
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
		_ func(*WorktreeAdapter, context.Context, types.ScanOptions) ([]types.DebrisInfo, error) = (*WorktreeAdapter).Scan
		_ func() *WorktreeAdapter                                                                = NewWorktreeAdapter
		_ func(*WorktreeAdapter) types.Tool                                                      = (*WorktreeAdapter).Name
		_ func(*WorktreeAdapter) types.Category                                                  = (*WorktreeAdapter).Category
		_ func(string, []registeredWorktreeContainer) bool                                       = isWorktreeContainerMember
		_ func(string, []registeredWorktreeContainer) worktreeRoot                               = worktreeUnitMeta
		_ func(context.Context, []types.DebrisInfo, string) ([]types.DebrisInfo, error)          = applyWorktreeUnitSizes
	)

	worktreeSource := readAdapterSource(t, "worktree.go")
	helperSource := readAdapterSource(t, "worktree_helpers.go")
	if !strings.Contains(worktreeSource, "func (a *WorktreeAdapter) Scan(") {
		t.Error("Scan is not defined in worktree.go")
	}
	if !strings.Contains(worktreeSource, "func (a *WorktreeAdapter) scanWorktreeRoots(") {
		t.Error("scanWorktreeRoots is not defined in worktree.go")
	}
	if !strings.Contains(worktreeSource, "a.scanExplicitRootUnits(") {
		t.Error("worktree.go no longer delegates to scanExplicitRootUnits")
	}
	for _, name := range []string{
		"scanExplicitRootUnits",
		"scanRootAsWorktreeUnit",
		"linkedWorktreeOwnerAt",
		"isWorktreeContainerMember",
		"worktreeUnitMeta",
		"scanWorktreeUnit",
		"applyWorktreeUnitSizes",
	} {
		if strings.Contains(worktreeSource, "func "+name+"(") || strings.Contains(worktreeSource, "func (a *WorktreeAdapter) "+name+"(") {
			t.Errorf("%s is still defined in worktree.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") && !strings.Contains(helperSource, "func (a *WorktreeAdapter) "+name+"(") {
			t.Errorf("%s is not defined in worktree_helpers.go", name)
		}
	}
}
