package cleaner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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
				path := testutil.UVCache(home)
				if err := os.MkdirAll(path, 0o755); err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(t.TempDir(), "sentinel")
				if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
				binDir := t.TempDir()
				t.Setenv("PATH", binDir)
				if route == "command" {
					writeExecutable(t, filepath.Join(binDir, "uv"), fakeCommand{mode: "write-file", file: outside, content: "changed"})
				}
				item := types.DebrisInfo{ID: "uv", Tool: types.ToolPipCache, Category: types.CategoryOtherCache, Path: path,
					CleanupKind: types.CleanupCommand, CleanupCommand: []string{"uv", "cache", "clean"}}
				calls := 0
				barrier := func(context.Context, types.DebrisInfo) error {
					calls++
					if route == "fallback" && calls == 1 {
						return nil
					}
					if drift == "catalog target" {
						t.Setenv("UV_CACHE_DIR", filepath.Dir(outside))
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
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := testutil.UVCache(home)
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
	writeExecutable(t, filepath.Join(binDir, "uv"), fakeCommand{mode: "exit", exitCode: 7})
	item := types.DebrisInfo{ID: "uv", Tool: types.ToolPipCache, Category: types.CategoryOtherCache, Path: path,
		CleanupKind: types.CleanupCommand, CleanupCommand: []string{"uv", "cache", "clean"}}
	if total, err := Execute([]types.DebrisInfo{item}); err == nil || total != 0 {
		t.Fatalf("failure = total %d err %v", total, err)
	}
	for _, sentinel := range []string{filepath.Join(path, "payload"), outside} {
		if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
			t.Errorf("sentinel changed: %q %v", data, err)
		}
	}
}
