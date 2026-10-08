package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanJSONReceiptRefusesSymlinkedCacheLeaf(t *testing.T) {
	for _, cache := range []string{"npm", "homebrew"} {
		t.Run(cache, func(t *testing.T) {
			if cache == "homebrew" && runtime.GOOS != "darwin" {
				t.Skip("Homebrew catalog entry is macOS-only")
			}
			home := t.TempDir()
			testutil.SetHome(t, home)
			// No real package manager can run, even on an incorrect route.
			t.Setenv("PATH", t.TempDir())
			path := filepath.Join(home, ".npm", "_cacache")
			if runtime.GOOS == "windows" {
				path = filepath.Join(home, ".cache", "npm-cache", "_cacache")
			}
			if cache == "homebrew" {
				path = filepath.Join(home, "Library", "Caches", "Homebrew")
			}
			elsewhere := filepath.Join(home, "elsewhere", filepath.Base(path))
			payload := strings.Repeat("x", 5000)
			writeJSONReceiptFixture(t, elsewhere, payload)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
			if err != nil || len(items) != 1 || items[0].Size != 5000 {
				t.Fatalf("scan = %+v, %v", items, err)
			}
			plan, document, components, prepared := preparedReceiptFixture(t, items, staticOverlapSafetyRuntime(nil, nil))
			receipt, err := executeCleanJSONReceipt(context.Background(), confirminput.NewReader(strings.NewReader("")), document, components, plan, prepared, true, false)
			if err == nil || !strings.Contains(err.Error(), "cache leaf is a symlink") || receipt.Status != cleanJSONReceiptFailed ||
				receipt.Totals.Requested != 1 || receipt.Totals.Failed != 1 || receipt.Totals.FreedBytes != 0 {
				t.Errorf("refusal receipt=%+v error=%v", receipt, err)
			}
			if len(receipt.PhysicalTargets) != 1 {
				t.Fatalf("receipt targets = %+v", receipt.PhysicalTargets)
			}
			target := receipt.PhysicalTargets[0]
			if target.State != "failed" || target.PhysicalRemoved || target.FreedBytes != 0 {
				t.Errorf("refusal target = %+v", target)
			}
			encoded, err := json.Marshal(receipt)
			if err != nil || !strings.Contains(string(encoded), "cache_leaf_symlink") {
				t.Errorf("JSON lacks clear refusal: %s, %v", encoded, err)
			}
			if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("refused link changed: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(elsewhere, "payload")); err != nil || string(data) != payload {
				t.Errorf("target payload changed: %d bytes, %v", len(data), err)
			}
		})
	}
}
