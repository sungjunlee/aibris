package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func excludeTestProvider(home string) *mockProvider {
	item := func(id string) types.DebrisInfo {
		return types.DebrisInfo{
			ID:       id,
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			Path:     filepath.Join(home, "work", id, "node_modules"),
			Size:     100,
		}
	}
	return &mockProvider{
		name: types.ToolNodeModules,
		worktrees: []types.DebrisInfo{
			item("keep"),
			item("hidden-flag"),
			item("hidden-file"),
		},
	}
}

func TestScanWithOptions_ExcludesRemoveDiscoveredItems(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New([]adapter.DebrisProvider{excludeTestProvider(resolvedHome)})

	result, err := s.ScanWithOptions(context.Background(), types.ScanOptions{
		Roots:    []string{home},
		Excludes: []string{filepath.Join(resolvedHome, "work", "hidden-flag")},
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalCount != 2 {
		t.Fatalf("TotalCount = %d; want 2 (keep + hidden-file retained, hidden-flag excluded)", result.TotalCount)
	}
	ids := map[string]bool{}
	for _, w := range result.Worktrees {
		ids[w.ID] = true
	}
	if !ids["keep"] || !ids["hidden-file"] || ids["hidden-flag"] {
		t.Fatalf("worktrees = %+v; want keep + hidden-file retained, hidden-flag excluded", result.Worktrees)
	}
	if result.ExcludedByUser != 1 {
		t.Errorf("ExcludedByUser = %d; want 1", result.ExcludedByUser)
	}
	if len(result.ExcludedScopes) != 1 || result.ExcludedScopes[0].Count != 1 {
		t.Fatalf("ExcludedScopes = %+v; want one scope with count 1", result.ExcludedScopes)
	}
	if result.ExcludedScopes[0].Source != types.ExcludeSourceFlag {
		t.Errorf("scope source = %q; want flag", result.ExcludedScopes[0].Source)
	}
	if len(result.RejectedExcludes) != 0 {
		t.Errorf("RejectedExcludes = %+v; want none", result.RejectedExcludes)
	}
}

func TestScanWithOptions_ExcludesOutsideRootsNotHonored(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	s := New([]adapter.DebrisProvider{excludeTestProvider(resolvedHome)})
	traversal := resolvedHome + string(filepath.Separator) + "work" +
		string(filepath.Separator) + ".." + string(filepath.Separator) + "elsewhere"

	result, err := s.ScanWithOptions(context.Background(), types.ScanOptions{
		Roots:    []string{home},
		Excludes: []string{filepath.Join(outside, "target"), traversal},
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalCount != 3 || result.ExcludedByUser != 0 {
		t.Fatalf("count/excluded = %d/%d; want every item retained", result.TotalCount, result.ExcludedByUser)
	}
	if len(result.ExcludedScopes) != 0 {
		t.Errorf("ExcludedScopes = %+v; want none", result.ExcludedScopes)
	}
	if len(result.RejectedExcludes) != 2 {
		t.Fatalf("RejectedExcludes = %+v; want 2", result.RejectedExcludes)
	}
	for _, rejected := range result.RejectedExcludes {
		if rejected.Reason != "outside scan roots" {
			t.Errorf("reason = %q; want outside scan roots", rejected.Reason)
		}
		if rejected.Source != types.ExcludeSourceFlag {
			t.Errorf("source = %q; want flag", rejected.Source)
		}
	}
}

func TestScanWithOptions_IgnoreFileMergesWithFlags(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New([]adapter.DebrisProvider{excludeTestProvider(resolvedHome)})

	ignoreFile := exclude.UserIgnoreFile()
	if err := os.MkdirAll(filepath.Dir(ignoreFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoreFile, []byte(filepath.Join(resolvedHome, "work", "hidden-file")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := s.ScanWithOptions(context.Background(), types.ScanOptions{
		Roots:    []string{home},
		Excludes: []string{filepath.Join(resolvedHome, "work", "hidden-flag")},
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalCount != 1 || result.Worktrees[0].ID != "keep" {
		t.Fatalf("worktrees = %+v; want only the kept item", result.Worktrees)
	}
	if result.ExcludedByUser != 2 {
		t.Fatalf("ExcludedByUser = %d; want 2", result.ExcludedByUser)
	}
	sources := map[types.ExcludeSource]int{}
	for _, scope := range result.ExcludedScopes {
		sources[scope.Source]++
	}
	if sources[types.ExcludeSourceFlag] != 1 || sources[types.ExcludeSourceIgnoreFile] != 1 {
		t.Errorf("scope sources = %+v; want one flag and one ignore-file", sources)
	}
}

func TestScanWithOptions_DefaultsUnchangedWithoutExclusions(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	s := New([]adapter.DebrisProvider{excludeTestProvider(resolvedHome)})

	result, err := s.ScanWithOptions(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalCount != 3 {
		t.Fatalf("TotalCount = %d; want all 3 items", result.TotalCount)
	}
	if result.ExcludedByUser != 0 || result.ExcludedScopes != nil || result.RejectedExcludes != nil {
		t.Errorf("exclusion fields = %d/%+v/%+v; want zero values without configuration",
			result.ExcludedByUser, result.ExcludedScopes, result.RejectedExcludes)
	}
}
