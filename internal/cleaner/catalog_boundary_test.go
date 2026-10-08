package cleaner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteRevalidatesCommandAndFallbackAuthorityAtBarrier(t *testing.T) {
	for _, route := range []string{"command", "fallback"} {
		for _, drift := range []string{"catalog target", "deletion gate"} {
			t.Run(route+"/"+drift, func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				path := testutil.GoBuildCache(home)
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "sentinel")
				if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
				binDir := t.TempDir()
				if route == "command" {
					if runtime.GOOS == "windows" {
						t.Skip("shell executable fixture is Unix-specific")
					}
					writeExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nprintf changed > '"+outside+"'\n")
				}
				t.Setenv("PATH", binDir)
				item := types.DebrisInfo{ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, Path: path,
					CleanupKind: types.CleanupCommand, CleanupCommand: []string{"go", "clean", "-cache"}}
				calls := 0
				barrier := func(context.Context, types.DebrisInfo) error {
					calls++
					if route == "fallback" && calls == 1 {
						return nil
					}
					if drift == "catalog target" {
						t.Setenv("GOCACHE", filepath.Dir(outside))
					} else if err := os.Mkdir(filepath.Join(path, ".git"), 0o755); err != nil {
						t.Fatal(err)
					}
					return nil
				}
				attempted := false
				var diagnostics bytes.Buffer
				total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(context.Background(), []types.DebrisInfo{item}, barrier,
					&diagnostics, &diagnostics, func(outcome CleanupMutationOutcome) { attempted = outcome.MutationAttempted })
				if err == nil || total != 0 || attempted {
					t.Errorf("barrier refusal = total %d attempted %t err %v", total, attempted, err)
				}
				if drift == "catalog target" && !errors.Is(err, ErrCleanupRecipeChanged) {
					t.Errorf("catalog drift = %v; want stable recipe refusal", err)
				}
				wantCalls := 1
				if route == "fallback" {
					wantCalls = 2
				}
				if calls != wantCalls {
					t.Errorf("barrier calls = %d; want %d", calls, wantCalls)
				}
				if _, err := os.Stat(path); err != nil {
					t.Errorf("refused cache changed: %v", err)
				}
				if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
					t.Errorf("outside sentinel changed: %q %v", data, err)
				}
			})
		}
	}
}

func TestExecuteCommandFailurePreservesOutsideSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture is Unix-specific")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := testutil.GoBuildCache(home)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "payload"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nexit 7\n")
	t.Setenv("PATH", binDir)
	item := types.DebrisInfo{ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, Path: path,
		CleanupKind: types.CleanupCommand, CleanupCommand: []string{"go", "clean", "-cache"}}
	if total, err := Execute([]types.DebrisInfo{item}); err == nil || total != 0 {
		t.Fatalf("failure = total %d err %v", total, err)
	}
	for _, sentinel := range []string{filepath.Join(path, "payload"), outside} {
		if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
			t.Errorf("sentinel changed: %q %v", data, err)
		}
	}
}
