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
)

// Compile-time re-export identity: the original cache/index names still resolve
// in package cmd after the same-package extract.
var (
	_ = codexActivityCachePath
	_ = readCodexActivityCache
	_ = saveCodexActivityCache
	_ = refreshCodexActivityCache
	_ = aggregateCodexActivity
	_ = aggregateCodexMemberActivity
	_ = indexFromCodexActivityCache
	_ = unavailableCodexActivityIndex
	_ = codexActivityMemberKey
	_ = loadCodexActivityIndex
	_ = loadCodexActivityIndexWithOptions
	_ = fillCodexActivityIndexOptions
	_ = loadCodexActivityRecommendations
	_ = recommendCodexActivityWorktrees
	_ = activeCodexWorktrees
	_ = activeWorktrees
	_ = isActiveCodexWorktree
	_ = defaultCodexSessionRoots
	_ = (*codexActivityCache).rebuildAggregates
)

func TestCodexActivityCacheLiveApartFromIndexLoader(t *testing.T) {
	cacheNames := []string{
		"codexActivityCachePath",
		"readCodexActivityCache",
		"saveCodexActivityCache",
		"refreshCodexActivityCache",
		"rebuildAggregates",
		"aggregateCodexActivity",
		"aggregateCodexMemberActivity",
		"indexFromCodexActivityCache",
		"unavailableCodexActivityIndex",
		"codexActivityMemberKey",
	}
	entryNames := []string{
		"loadCodexActivityIndex",
		"loadCodexActivityIndexWithOptions",
		"fillCodexActivityIndexOptions",
		"loadCodexActivityRecommendations",
		"recommendCodexActivityWorktrees",
		"activeCodexWorktrees",
		"activeWorktrees",
		"isActiveCodexWorktree",
		"defaultCodexSessionRoots",
		"ProjectHasSessionAfter",
	}
	cacheTypes := []string{
		"codexActivityCache",
		"codexActivityFileRecord",
	}
	entryTypes := []string{
		"codexActivityIndex",
		"codexActivityIndexOptions",
	}

	wanted := make(map[string]string, len(cacheNames)+len(entryNames)+len(cacheTypes)+len(entryTypes))
	for _, name := range cacheNames {
		wanted[name] = "codex_activity_cache.go"
	}
	for _, name := range entryNames {
		wanted[name] = "codex_activity.go"
	}
	for _, name := range cacheTypes {
		wanted[name] = "codex_activity_cache.go"
	}
	for _, name := range entryTypes {
		wanted[name] = "codex_activity.go"
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

func TestCodexActivityCacheReexportIdentity(t *testing.T) {
	// Same-package split: cache identifiers keep their original names so
	// existing cmd callers still resolve to the implementations.
	var (
		_ func() (string, error)                                                                                       = codexActivityCachePath
		_ func(string) (codexActivityCache, bool, error)                                                               = readCodexActivityCache
		_ func(string, codexActivityCache) error                                                                       = saveCodexActivityCache
		_ func(context.Context, codexActivityIndexOptions, codexActivityCache, bool) (codexActivityCache, error)       = refreshCodexActivityCache
		_ func(*codexActivityCache)                                                                                    = (*codexActivityCache).rebuildAggregates
		_ func(map[string]codexActivityFileRecord) (map[string]codexWorktreeActivity, map[string]codexProjectActivity) = aggregateCodexActivity
		_ func(map[string]codexActivityFileRecord) map[string]codexWorktreeActivity                                    = aggregateCodexMemberActivity
		_ func(codexActivityCache, time.Duration, string, error) codexActivityIndex                                    = indexFromCodexActivityCache
		_ func(error) codexActivityIndex                                                                               = unavailableCodexActivityIndex
		_ func(string, string) string                                                                                  = codexActivityMemberKey
		_ func(context.Context) codexActivityIndex                                                                     = loadCodexActivityIndex
		_ func(context.Context, codexActivityIndexOptions) codexActivityIndex                                          = loadCodexActivityIndexWithOptions
		_ func(codexActivityIndexOptions) codexActivityIndexOptions                                                    = fillCodexActivityIndexOptions
		_ func(context.Context, []types.DebrisInfo) codexActivityRecommendationPlan                                    = loadCodexActivityRecommendations
		_ func([]types.DebrisInfo, codexActivityIndex) codexActivityRecommendationPlan                                 = recommendCodexActivityWorktrees
		_ func([]types.DebrisInfo) []types.DebrisInfo                                                                  = activeCodexWorktrees
		_ func([]types.DebrisInfo) []types.DebrisInfo                                                                  = activeWorktrees
		_ func(types.DebrisInfo) bool                                                                                  = isActiveCodexWorktree
		_ func() ([]string, error)                                                                                     = defaultCodexSessionRoots
	)

	original := readCmdSource(t, "codex_activity.go")
	for _, name := range []string{
		"loadCodexActivityIndex",
		"loadCodexActivityIndexWithOptions",
		"fillCodexActivityIndexOptions",
		"loadCodexActivityRecommendations",
		"recommendCodexActivityWorktrees",
		"defaultCodexSessionRoots",
	} {
		if !strings.Contains(original, "func "+name+"(") {
			t.Errorf("%s is not defined in codex_activity.go", name)
		}
	}
	if !strings.Contains(original, "func (i codexActivityIndex) ProjectHasSessionAfter(") {
		t.Error("ProjectHasSessionAfter is not defined in codex_activity.go")
	}
	for _, name := range []string{
		"codexActivityCachePath",
		"readCodexActivityCache",
		"saveCodexActivityCache",
		"refreshCodexActivityCache",
		"indexFromCodexActivityCache",
		"unavailableCodexActivityIndex",
	} {
		if strings.Contains(original, "func "+name+"(") {
			t.Errorf("%s is still defined in codex_activity.go", name)
		}
		if !strings.Contains(original, name+"(") {
			t.Errorf("codex_activity.go no longer delegates to %s", name)
		}
	}
	for _, name := range []string{
		"aggregateCodexActivity",
		"aggregateCodexMemberActivity",
		"codexActivityMemberKey",
	} {
		if strings.Contains(original, "func "+name+"(") {
			t.Errorf("%s is still defined in codex_activity.go", name)
		}
	}
	if strings.Contains(original, "func (c *codexActivityCache) rebuildAggregates(") {
		t.Error("rebuildAggregates is still defined in codex_activity.go")
	}
	if !strings.Contains(original, ".rebuildAggregates(") {
		t.Error("codex_activity.go no longer delegates to rebuildAggregates")
	}
}
