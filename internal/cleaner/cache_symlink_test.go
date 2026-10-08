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

func TestCachePathRemovalRefusesSymlinkIntroducedAtBarrier(t *testing.T) {
	for _, cache := range []string{"npm", "go-build"} {
		t.Run(cache, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, ".npm", "_cacache")
			if runtime.GOOS == "windows" {
				path = filepath.Join(home, ".cache", "npm-cache", "_cacache")
			}
			if cache == "go-build" {
				path = testutil.GoBuildCache(home)
			}
			elsewhere := filepath.Join(home, "elsewhere")
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			payloadName := "payload"
			if cache == "go-build" {
				payloadName = "log.txt"
			}
			payload := strings.Repeat("x", 5000)
			if err := os.WriteFile(filepath.Join(path, payloadName), []byte(payload), 0o644); err != nil {
				t.Fatal(err)
			}
			items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
			if err != nil || len(items) != 1 {
				t.Fatalf("scan = %+v, %v", items, err)
			}
			barrier := func(context.Context, types.DebrisInfo) error {
				if err := os.Rename(path, elsewhere); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(elsewhere, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				return nil
			}
			attempted := false
			var output bytes.Buffer
			total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(context.Background(), items, barrier,
				&output, &output, func(outcome CleanupMutationOutcome) { attempted = attempted || outcome.MutationAttempted })
			if err == nil || !strings.Contains(err.Error(), "cache leaf is a symlink") || total != 0 || attempted {
				t.Errorf("refusal = total %d attempted %t err %v", total, attempted, err)
			}
			if !strings.Contains(output.String(), "cache leaf is a symlink") || strings.Contains(output.String(), "removed:") {
				t.Errorf("human output must report refusal: %s", output.String())
			}
			if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("refused link changed: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(elsewhere, payloadName)); err != nil || string(data) != payload {
				t.Errorf("target payload changed: %d bytes, %v", len(data), err)
			}
		})
	}
}
