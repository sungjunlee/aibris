package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestBuildWorktreeCleanupUnits(t *testing.T) {
	tests := []struct {
		name        string
		buildItems  func(t *testing.T, root string) []types.DebrisInfo
		wantUnits   int
		wantTargets []string
		wantMembers [][]string
		wantSizes   []int64
		wantSources []string
	}{
		{
			name: "direct member",
			buildItems: func(t *testing.T, root string) []types.DebrisInfo {
				target := filepath.Join(root, "worktrees", "direct")
				createCleanupUnitGitFile(t, target, "direct")
				return []types.DebrisInfo{cleanupUnitItem(target, 100, ".claude")}
			},
			wantUnits:   1,
			wantTargets: []string{"worktrees/direct"},
			wantMembers: [][]string{{"worktrees/direct"}},
			wantSizes:   []int64{100},
			wantSources: []string{".claude"},
		},
		{
			name: "nested member",
			buildItems: func(t *testing.T, root string) []types.DebrisInfo {
				target := filepath.Join(root, "worktrees", "nested")
				createCleanupUnitGitFile(t, filepath.Join(target, "project"), "nested")
				return []types.DebrisInfo{cleanupUnitItem(target, 200, ".codex")}
			},
			wantUnits:   1,
			wantTargets: []string{"worktrees/nested"},
			wantMembers: [][]string{{"worktrees/nested/project"}},
			wantSizes:   []int64{200},
			wantSources: []string{".codex"},
		},
		{
			name: "duplicate rows count size once",
			buildItems: func(t *testing.T, root string) []types.DebrisInfo {
				target := filepath.Join(root, "worktrees", "duplicate")
				createCleanupUnitGitFile(t, filepath.Join(target, "project"), "duplicate")
				return []types.DebrisInfo{
					cleanupUnitItem(target, 275, ".relay"),
					cleanupUnitItem(target, 300, ".codex"),
				}
			},
			wantUnits:   1,
			wantTargets: []string{"worktrees/duplicate"},
			wantMembers: [][]string{{"worktrees/duplicate/project"}},
			wantSizes:   []int64{300},
			wantSources: []string{".codex"},
		},
		{
			name: "two nested members",
			buildItems: func(t *testing.T, root string) []types.DebrisInfo {
				target := filepath.Join(root, "worktrees", "two")
				createCleanupUnitGitFile(t, filepath.Join(target, "project-b"), "two-b")
				createCleanupUnitGitFile(t, filepath.Join(target, "project-a"), "two-a")
				return []types.DebrisInfo{
					cleanupUnitItem(target, 400, ".relay"),
					cleanupUnitItem(target, 400, ".relay"),
				}
			},
			wantUnits:   1,
			wantTargets: []string{"worktrees/two"},
			wantMembers: [][]string{{"worktrees/two/project-a", "worktrees/two/project-b"}},
			wantSizes:   []int64{400},
			wantSources: []string{".relay"},
		},
		{
			name: "units ordered by canonical target",
			buildItems: func(t *testing.T, root string) []types.DebrisInfo {
				targetB := filepath.Join(root, "worktrees", "b")
				targetA := filepath.Join(root, "worktrees", "a")
				createCleanupUnitGitFile(t, targetB, "b")
				createCleanupUnitGitFile(t, targetA, "a")
				return []types.DebrisInfo{
					cleanupUnitItem(targetB, 20, ".codex"),
					cleanupUnitItem(targetA, 10, ".codex"),
				}
			},
			wantUnits:   2,
			wantTargets: []string{"worktrees/a", "worktrees/b"},
			wantMembers: [][]string{{"worktrees/a"}, {"worktrees/b"}},
			wantSizes:   []int64{10, 20},
			wantSources: []string{".codex", ".codex"},
		},
		{
			name: "irrelevant non-worktree input",
			buildItems: func(t *testing.T, root string) []types.DebrisInfo {
				target := filepath.Join(root, "node_modules")
				createCleanupUnitGitFile(t, target, "irrelevant")
				return []types.DebrisInfo{{Category: types.CategoryNodeModules, Path: target, Size: 500}}
			},
			wantUnits: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			root, _ = cleanTargetPathKey(root)
			items := tt.buildItems(t, root)

			units, err := BuildWorktreeCleanupUnits(items)
			if err != nil {
				t.Fatal(err)
			}
			if len(units) != tt.wantUnits {
				t.Fatalf("units = %d; want %d (%+v)", len(units), tt.wantUnits, units)
			}
			for i, unit := range units {
				if got := relativeCleanupUnitPath(t, root, unit.TargetPath); got != tt.wantTargets[i] {
					t.Errorf("unit[%d].TargetPath = %q; want %q", i, got, tt.wantTargets[i])
				}
				gotMembers := make([]string, 0, len(unit.Members))
				for _, member := range unit.Members {
					gotMembers = append(gotMembers, relativeCleanupUnitPath(t, root, member.WorktreePath))
				}
				if !reflect.DeepEqual(gotMembers, tt.wantMembers[i]) {
					t.Errorf("unit[%d].Members = %v; want %v", i, gotMembers, tt.wantMembers[i])
				}
				if unit.Size != tt.wantSizes[i] {
					t.Errorf("unit[%d].Size = %d; want %d", i, unit.Size, tt.wantSizes[i])
				}
				if unit.Source != tt.wantSources[i] {
					t.Errorf("unit[%d].Source = %q; want %q", i, unit.Source, tt.wantSources[i])
				}
			}

			reversed := append([]types.DebrisInfo(nil), items...)
			for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
				reversed[left], reversed[right] = reversed[right], reversed[left]
			}
			gotAgain, err := BuildWorktreeCleanupUnits(reversed)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(units, gotAgain) {
				t.Fatalf("units depend on scanner row order:\nfirst:  %+v\nsecond: %+v", units, gotAgain)
			}
		})
	}
}

func TestBuildWorktreeCleanupUnitsCanonicalizesSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	root, _ = cleanTargetPathKey(root)
	realTarget := filepath.Join(root, "real", "worktree")
	createCleanupUnitGitFile(t, filepath.Join(realTarget, "project"), "canonical")
	aliasTarget := filepath.Join(root, "alias")
	if err := os.Symlink(realTarget, aliasTarget); err != nil {
		t.Fatal(err)
	}

	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{
		cleanupUnitItem(aliasTarget, 600, ".codex"),
		cleanupUnitItem(realTarget, 600, ".codex"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("units = %d; want 1 (%+v)", len(units), units)
	}
	if units[0].TargetPath != realTarget {
		t.Errorf("TargetPath = %q; want canonical %q", units[0].TargetPath, realTarget)
	}
	wantMember := filepath.Join(realTarget, "project")
	if len(units[0].Members) != 1 || units[0].Members[0].WorktreePath != wantMember {
		t.Errorf("Members = %+v; want %q", units[0].Members, wantMember)
	}
}

func createCleanupUnitGitFile(t *testing.T, worktreePath, name string) {
	t.Helper()
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(t.TempDir(), ".git", "worktrees", name)
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("gitdir: "+gitDir+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func cleanupUnitItem(path string, size int64, source string) types.DebrisInfo {
	return types.DebrisInfo{
		Category: types.CategoryWorktree,
		Path:     path,
		Size:     size,
		Source:   source,
		Status:   types.WorktreeActive,
	}
}

func relativeCleanupUnitPath(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}
