package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestOrcaContainersSkipNonContainersAndDoNotCrawl(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	workspace := filepath.Join(home, "orca", "workspaces")
	createWorktreeGit(t, filepath.Join(workspace, "valid", "member"), filepath.Join(home, "parent"), "member")
	createWorktreeGit(t, filepath.Join(workspace, "checkout"), filepath.Join(home, "parent"), "checkout")
	createWorktreeGit(t, filepath.Join(workspace, "checkout", "nested"), filepath.Join(home, "parent"), "nested")
	if err := os.MkdirAll(filepath.Join(workspace, "primary", ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	createWorktreeGit(t, filepath.Join(workspace, "group", "deeper", "repo", "member"), filepath.Join(home, "parent"), "deep")
	if err := os.WriteFile(filepath.Join(workspace, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workspace, "valid"), filepath.Join(workspace, "alias")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	containers, err := registeredWorktreeContainers(canonicalExistingPath(home), []string{home})
	if err != nil {
		t.Fatal(err)
	}
	var orca []string
	for _, container := range containers {
		if container.source == "orca" {
			orca = append(orca, filepath.Base(container.relativePath))
		}
	}
	if strings.Join(orca, ",") != "group,valid" {
		t.Fatalf("Orca containers = %v; want only immediate non-checkout directories", orca)
	}
}

func TestOrcaUnreadableWorkspaceIsProviderErrorOnlyWhenSelected(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	workspace := filepath.Join(home, "orca", "workspaces")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workspace, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workspace, 0755) })
	if _, err := os.ReadDir(workspace); err == nil {
		t.Skip("platform/privileges do not enforce directory permissions")
	}
	if _, err := NewWorktreeAdapter().Scan(context.Background(), types.ScanOptions{}); err == nil {
		t.Fatal("unreadable registered workspace must be a provider error")
	}
	scoped := types.ScanOptions{Roots: []string{filepath.Join(home, "elsewhere")}, ExplicitRoots: true}
	if _, err := NewWorktreeAdapter().Scan(context.Background(), scoped); err != nil {
		t.Fatalf("unselected unreadable workspace must not affect explicit root: %v", err)
	}
}

func TestOrcaExplicitRootIgnoresUnreadableSiblingContainer(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	workspace := filepath.Join(home, "orca", "workspaces")
	owner := filepath.Join(workspace, "selected", "member")
	createWorktreeGit(t, owner, filepath.Join(home, "missing"), "member")
	blocked := filepath.Join(workspace, "blocked")
	if err := os.Mkdir(blocked, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0755) })
	if _, err := os.Lstat(filepath.Join(blocked, ".git")); !os.IsPermission(err) {
		t.Skip("platform/privileges do not enforce directory permissions")
	}
	rows, err := NewWorktreeAdapter().Scan(context.Background(), types.ScanOptions{Roots: []string{owner}, ExplicitRoots: true})
	if err != nil || len(rows) != 1 {
		t.Fatalf("explicit owner rows = %+v, %v; unselected sibling must not affect scope", rows, err)
	}
}

func TestOrcaCodexHomeFeedsRegistryLogsAndRootBoundary(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	alias := filepath.Join(home, "orca-home-alias")
	if err := os.Symlink(orca, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	owner := filepath.Join(orca, "worktrees", "member")
	createWorktreeGit(t, owner, filepath.Join(home, "missing"), "member")
	if err := os.Mkdir(filepath.Join(orca, "archived_sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orca, "logs_2.sqlite"), []byte("logs"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []DebrisProvider{NewWorktreeAdapter(), &AILogsAdapter{}} {
		rows, err := provider.Scan(context.Background(), types.ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if provider.Category() == types.CategoryAILogs {
			want = 2
		}
		if len(rows) != want {
			t.Fatalf("%s rows = %+v; want %d from auto-discovered home", provider.Category(), rows, want)
		}
		// Listing Orca explicitly must not create duplicate rows or change IDs.
		for _, configured := range []string{orca, alias} {
			t.Setenv("AIBRIS_CODEX_HOMES", configured)
			deduped, err := provider.Scan(context.Background(), types.ScanOptions{})
			if err != nil || len(deduped) != want {
				t.Fatalf("deduped rows = %+v, %v", deduped, err)
			}
		}
		t.Setenv("AIBRIS_CODEX_HOMES", "")
		scoped := types.ScanOptions{Roots: []string{filepath.Join(home, "elsewhere")}, ExplicitRoots: true}
		rows, err = provider.Scan(context.Background(), scoped)
		if err != nil || len(rows) != 0 {
			t.Fatalf("explicit root rows = %+v, %v; want no scope widening", rows, err)
		}
		warning, err := UncoveredCodexHomeWarning(scoped)
		if err != nil || warning == "" || strings.Contains(warning, "\n") {
			t.Fatalf("warning = %q, %v; want one-line diagnostic", warning, err)
		}
	}
	// Invalid layout silently drops discovery, including populated logs.
	if err := os.Remove(filepath.Join(orca, "config.toml")); err != nil {
		t.Fatal(err)
	}
	rows, err := (&AILogsAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil || len(rows) != 0 {
		t.Fatalf("invalid layout logs = %+v, %v; want ignored", rows, err)
	}
}
