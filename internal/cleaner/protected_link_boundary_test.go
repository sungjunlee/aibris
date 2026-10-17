package cleaner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/safedelete"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteProtectedLinkRetargetAtMutationBoundary(t *testing.T) {
	for _, route := range []string{"path", "command"} {
		for _, referent := range []string{"owner", "descendant"} {
			t.Run(route+"/"+referent, func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				path := filepath.Join(home, ".codex", "worktrees", "orphan")
				item := types.DebrisInfo{
					ID: "orphan", Path: path, Tool: types.ToolCodex,
					Category: types.CategoryWorktree, Status: types.WorktreeOrphaned,
				}
				if route == "command" {
					path = testutil.UVCache(home)
					item = types.DebrisInfo{
						ID: "uv", Path: path, Tool: types.ToolPipCache, Category: types.CategoryOtherCache,
						CleanupKind: types.CleanupCommand, CleanupCommand: []string{"uv", "cache", "clean"},
					}
				}
				sentinel := filepath.Join(path, "personal", "sentinel")
				if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
					t.Fatal(err)
				}
				const payload = "protected personal data"
				if err := os.WriteFile(sentinel, []byte(payload), 0o644); err != nil {
					t.Fatal(err)
				}
				item.Size = int64(len(payload))
				if route == "command" {
					// The hermetic command would remove the sentinel if started.
					writeExecutable(t, filepath.Join(t.TempDir(), "uv"), fakeCommand{mode: "remove-file", file: sentinel})
				}
				initial := filepath.Join(home, "initial-protected-data")
				if err := os.Mkdir(initial, 0o755); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(home, ".ssh")
				if err := os.Symlink(initial, link); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				if err := safedelete.Check(home, path); err != nil {
					t.Fatalf("candidate must pass the initial safety gate: %v", err)
				}
				barrierCalls := 0
				barrier := func(context.Context, types.DebrisInfo) error {
					barrierCalls++
					if err := os.Remove(link); err != nil {
						return err
					}
					target := path
					if referent == "descendant" {
						target = filepath.Dir(sentinel)
					}
					return os.Symlink(target, link)
				}
				var output bytes.Buffer
				var lastOutcome CleanupMutationOutcome
				total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(context.Background(), []types.DebrisInfo{item}, barrier,
					&output, &output, func(outcome CleanupMutationOutcome) { lastOutcome = outcome })
				if barrierCalls != 1 || !errors.Is(err, safedelete.ErrRefused) || total != 0 {
					t.Errorf("retarget cleanup = calls %d, freed %d, err %v; want one barrier and safety refusal with zero freed", barrierCalls, total, err)
				}
				if lastOutcome.FreedBytes != 0 || lastOutcome.ResidualBytes != int64(len(payload)) {
					t.Errorf("retarget outcome claims protected bytes were freed: %+v", lastOutcome)
				}
				if strings.Contains(output.String(), "removed:") || strings.Contains(output.String(), "cleaned:") {
					t.Errorf("success reported for refused cleanup: %s", output.String())
				}
				if data, err := os.ReadFile(sentinel); err != nil || string(data) != payload {
					t.Errorf("protected sentinel changed after retarget: %q, %v", data, err)
				}
			})
		}
	}
}
