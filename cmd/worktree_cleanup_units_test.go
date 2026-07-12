package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestBuildWorktreeCleanupUnitsReturnsErrorWhenTargetDisappears(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "worktrees", "disappeared")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	item := cleanupUnitItem(target, 100, ".codex")
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}

	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{item})
	if err == nil {
		t.Fatal("BuildWorktreeCleanupUnits() error = nil; want target filesystem error")
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("BuildWorktreeCleanupUnits() error = %q; want target path %q", err, target)
	}
	if len(units) != 0 {
		t.Errorf("BuildWorktreeCleanupUnits() units = %+v; want no units", units)
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

func TestBuildWorktreeCleanupUnitsUsesCanonicalRepositoryIdentity(t *testing.T) {
	root := t.TempDir()
	root, _ = cleanTargetPathKey(root)
	repositoryPath := filepath.Join(root, "repositories", "canonical-name")
	commonDir := filepath.Join(repositoryPath, ".git")
	aliasPath := filepath.Join(root, "aliases", "display-alias")
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(repositoryPath, aliasPath); err != nil {
		t.Fatal(err)
	}

	first := filepath.Join(root, "worktrees", "feature-one")
	second := filepath.Join(root, "worktrees", "unrelated-worktree-name")
	createCleanupUnitLinkedWorktree(t, first, commonDir, "first", commonDir)
	createCleanupUnitLinkedWorktree(t, second, commonDir, "second", filepath.Join(aliasPath, ".git"))

	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{
		cleanupUnitItem(second, 200, ".codex"),
		cleanupUnitItem(first, 100, ".codex"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("units = %d; want 2 (%+v)", len(units), units)
	}
	for _, unit := range units {
		member := unit.Members[0]
		if member.RepositoryID != commonDir {
			t.Errorf("RepositoryID = %q; want canonical common-dir %q", member.RepositoryID, commonDir)
		}
		if member.DisplayRepository != "canonical-name" {
			t.Errorf("DisplayRepository = %q; want canonical-name", member.DisplayRepository)
		}
		if !member.EvidenceAvailable || member.EvidenceError != "" {
			t.Errorf("repository evidence = (%t, %q); want available", member.EvidenceAvailable, member.EvidenceError)
		}
	}
}

func TestBuildWorktreeCleanupUnitsKeepsSameBasenameRepositoriesDistinct(t *testing.T) {
	root := t.TempDir()
	firstCommonDir := filepath.Join(root, "owner-a", "project", ".git")
	secondCommonDir := filepath.Join(root, "owner-b", "project", ".git")
	first := filepath.Join(root, "worktrees", "first")
	second := filepath.Join(root, "worktrees", "second")
	createCleanupUnitLinkedWorktree(t, first, firstCommonDir, "first", firstCommonDir)
	createCleanupUnitLinkedWorktree(t, second, secondCommonDir, "second", secondCommonDir)

	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{
		cleanupUnitItem(first, 100, ".codex"),
		cleanupUnitItem(second, 200, ".codex"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("units = %d; want 2 (%+v)", len(units), units)
	}
	firstMember := units[0].Members[0]
	secondMember := units[1].Members[0]
	if firstMember.DisplayRepository != "project" || secondMember.DisplayRepository != "project" {
		t.Fatalf("display repositories = %q, %q; want project, project", firstMember.DisplayRepository, secondMember.DisplayRepository)
	}
	if firstMember.RepositoryID == secondMember.RepositoryID {
		t.Fatalf("same-basename repositories share identity %q", firstMember.RepositoryID)
	}
}

func TestBuildWorktreeCleanupUnitsOrdersMultipleRepositoriesDeterministically(t *testing.T) {
	root := t.TempDir()
	root, _ = cleanTargetPathKey(root)
	target := filepath.Join(root, "worktrees", "multi-repository")
	alphaPath := filepath.Join(target, "alpha-member")
	zetaPath := filepath.Join(target, "zeta-member")
	alphaCommonDir := filepath.Join(root, "repositories", "alpha", ".git")
	zetaCommonDir := filepath.Join(root, "repositories", "zeta", ".git")
	createCleanupUnitLinkedWorktree(t, zetaPath, zetaCommonDir, "zeta", zetaCommonDir)
	createCleanupUnitLinkedWorktree(t, alphaPath, alphaCommonDir, "alpha", alphaCommonDir)

	items := []types.DebrisInfo{
		cleanupUnitItem(target, 400, ".relay"),
		cleanupUnitItem(target, 400, ".codex"),
	}
	units, err := BuildWorktreeCleanupUnits(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || len(units[0].Members) != 2 {
		t.Fatalf("units = %+v; want one unit with two members", units)
	}
	wantIdentities := []struct {
		worktreePath      string
		repositoryID      string
		displayRepository string
	}{
		{alphaPath, alphaCommonDir, "alpha"},
		{zetaPath, zetaCommonDir, "zeta"},
	}
	for i, member := range units[0].Members {
		want := wantIdentities[i]
		if member.WorktreePath != want.worktreePath || member.RepositoryID != want.repositoryID || member.DisplayRepository != want.displayRepository || !member.EvidenceAvailable {
			t.Fatalf("member[%d] identity = %+v; want path=%q repository=%q display=%q available", i, member, want.worktreePath, want.repositoryID, want.displayRepository)
		}
	}

	items[0], items[1] = items[1], items[0]
	gotAgain, err := BuildWorktreeCleanupUnits(items)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(units, gotAgain) {
		t.Fatalf("multi-repository unit is not deterministic:\nfirst:  %+v\nsecond: %+v", units, gotAgain)
	}
}

func TestBuildWorktreeCleanupUnitsSurfacesRepositoryMetadataFailures(t *testing.T) {
	root := t.TempDir()
	ambiguousTarget := filepath.Join(root, "worktrees", "ambiguous")
	if err := os.MkdirAll(ambiguousTarget, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ambiguousTarget, ".git"), []byte("gitdir: one\ngitdir: two\n"), 0644); err != nil {
		t.Fatal(err)
	}

	unreadableTarget := filepath.Join(root, "worktrees", "unreadable")
	gitDir := filepath.Join(root, "metadata", "unreadable", "worktrees", "member")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../../missing.git\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unreadableTarget, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unreadableTarget, ".git"), []byte("gitdir: "+gitDir+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{
		cleanupUnitItem(unreadableTarget, 200, ".codex"),
		cleanupUnitItem(ambiguousTarget, 100, ".codex"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("units = %d; want 2 (%+v)", len(units), units)
	}

	wantFailure := []string{"ambiguous Git metadata", "unreadable Git metadata"}
	for i, unit := range units {
		member := unit.Members[0]
		if !unit.HardLocked || len(unit.HardLockReasons) != 1 || unit.HardLockReasons[0].Code != GitReasonEvidenceUnavailable {
			t.Errorf("unit[%d] hard safety = (%t, %+v); want evidence-unavailable lock", i, unit.HardLocked, unit.HardLockReasons)
		}
		if member.EvidenceAvailable {
			t.Errorf("unit[%d].EvidenceAvailable = true; want false", i)
		}
		if member.GitEvidenceAvailable || !member.HardLocked || member.Reason.Code != GitReasonEvidenceUnavailable {
			t.Errorf("unit[%d] Git evidence = %+v; want unavailable hard lock", i, member)
		}
		if member.RepositoryID != "" || member.DisplayRepository != "" {
			t.Errorf("unit[%d] repository identity = (%q, %q); want empty", i, member.RepositoryID, member.DisplayRepository)
		}
		if !strings.Contains(member.EvidenceError, wantFailure[i]) {
			t.Errorf("unit[%d].EvidenceError = %q; want %q", i, member.EvidenceError, wantFailure[i])
		}
	}
}

func createCleanupUnitLinkedWorktree(t *testing.T, worktreePath, commonDir, name, gitFileCommonDir string) {
	t.Helper()
	gitDir := filepath.Join(commonDir, "worktrees", name)
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatal(err)
	}
	gitFileGitDir := filepath.Join(gitFileCommonDir, "worktrees", name)
	if err := os.WriteFile(filepath.Join(worktreePath, ".git"), []byte("gitdir: "+gitFileGitDir+"\n"), 0644); err != nil {
		t.Fatal(err)
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
