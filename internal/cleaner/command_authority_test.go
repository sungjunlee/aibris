package cleaner

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteRefusesUntrustedCleanupRecipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture is Unix-specific")
	}
	for _, variant := range []string{"argv", "missing executable", "empty argv", "wrong tool", "wrong category", "wrong target", "removed recipe", "changed recipe", "wrong kind", "old Homebrew recipe"} {
		t.Run(variant, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, ".cache", "uv")
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			binDir := t.TempDir()
			// A tampered executable would mutate outside the previewed cache.
			writeExecutable(t, filepath.Join(binDir, "untrusted"), "#!/bin/sh\nprintf changed > '"+outside+"'\n")
			writeExecutable(t, filepath.Join(binDir, "uv"), "#!/bin/sh\nprintf changed > '"+outside+"'\n")
			t.Setenv("PATH", binDir)
			item := types.DebrisInfo{ID: "uv", Tool: types.ToolPipCache, Category: types.CategoryOtherCache, Path: path,
				CleanupKind: types.CleanupCommand, CleanupCommand: []string{"uv", "cache", "clean"}}
			switch variant {
			case "argv":
				item.CleanupCommand = []string{"untrusted"}
			case "missing executable":
				item.CleanupCommand = []string{"missing-untrusted"}
			case "empty argv":
				item.CleanupCommand = nil
			case "wrong tool":
				item.Tool = types.ToolBuildCache
			case "wrong category":
				item.Category = types.CategoryBuildCache
			case "wrong target":
				item.Path = filepath.Join(home, ".cache", "unknown")
			case "removed recipe":
				item.Path = filepath.Join(home, ".gradle", "caches")
				item.Tool, item.Category = types.ToolBuildCache, types.CategoryBuildCache
			case "changed recipe":
				item.CleanupCommand = []string{"uv", "cache", "clean", "--stale"}
			case "wrong kind":
				item.CleanupKind = types.CleanupRemovePath
			case "old Homebrew recipe":
				if runtime.GOOS != "darwin" {
					t.Skip("Homebrew catalog entry is macOS-only")
				}
				item.Path = filepath.Join(home, "Library", "Caches", "Homebrew")
				item.Tool, item.Category = types.ToolBuildCache, types.CategoryBuildCache
				item.CleanupCommand = []string{"brew", "cleanup", "--prune=all"}
				writeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nprintf changed > '"+outside+"'\n")
			}
			if err := os.MkdirAll(item.Path, 0o755); err != nil {
				t.Fatal(err)
			}
			var diagnostics bytes.Buffer
			attempted := false
			total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(context.Background(), []types.DebrisInfo{item}, nil,
				&diagnostics, &diagnostics, func(outcome CleanupMutationOutcome) { attempted = outcome.MutationAttempted })
			if err == nil || !strings.Contains(err.Error(), "cleanup recipe no longer matches live catalog") {
				t.Errorf("execution = %v; want stable catalog refusal", err)
			}
			if total != 0 || attempted {
				t.Errorf("refusal credited mutation: total=%d attempted=%t", total, attempted)
			}
			if !strings.Contains(diagnostics.String(), "cleanup recipe no longer matches live catalog") {
				t.Errorf("human output lacks refusal: %s", diagnostics.String())
			}
			if _, err := os.Stat(item.Path); err != nil {
				t.Errorf("refused cache must remain: %v", err)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
				t.Errorf("outside sentinel changed: %q %v", data, err)
			}
		})
	}
}

func TestHomebrewCleanupOnlyRemovesPreviewedCache(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Homebrew catalog entry is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, "Library", "Caches", "Homebrew")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "installed-keg")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nprintf changed > '"+outside+"'\n")
	t.Setenv("PATH", binDir)
	items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
	if err != nil || len(items) != 1 {
		t.Fatalf("Homebrew scan = %+v, %v", items, err)
	}
	if len(items[0].CleanupCommand) != 0 || items[0].CleanupKind == types.CleanupCommand {
		t.Errorf("Homebrew scan must use gated path removal: %+v", items[0])
	}
	if _, err := Execute(items); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("previewed cache remains: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Errorf("outside keg changed: %q %v", data, err)
	}
}
