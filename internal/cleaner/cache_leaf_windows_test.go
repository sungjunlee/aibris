//go:build windows

package cleaner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestWindowsCatalogCacheLeafEligibilityRefusesJunction(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, ".cache", "npm-cache", "_cacache")
	target := filepath.Join(home, "elsewhere")
	for _, dir := range []string{filepath.Dir(path), target} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "payload"), []byte(strings.Repeat("x", 5000)), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.WindowsJunction(t, path, target)
	items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil || len(items) != 1 || items[0].Size != 5000 {
		t.Fatalf("scan = %+v, %v", items, err)
	}
	opts := types.PruneOptions{Age: time.Hour, RelaxCacheAge: true}
	eligible, reason := EvaluateEligibility(items[0], opts, time.Now())
	if eligible || reason != EligibilityReasonCacheLeafSymlink || len(Filter(items, opts)) != 0 {
		t.Fatalf("junction selected: eligible=%t reason=%q filtered=%+v", eligible, reason, Filter(items, opts))
	}
}

func TestWindowsCachePathRemovalRefusesJunction(t *testing.T) {
	for _, introduced := range []string{"before-scan", "at-barrier"} {
		t.Run(introduced, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, ".cache", "npm-cache", "_cacache")
			target := filepath.Join(home, "elsewhere")
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			payload := strings.Repeat("x", 5000)
			if err := os.WriteFile(filepath.Join(path, "payload"), []byte(payload), 0o644); err != nil {
				t.Fatal(err)
			}
			var junctionInfo os.FileInfo
			introduceJunction := func(context.Context, types.DebrisInfo) error {
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
				testutil.WindowsJunction(t, path, target)
				var err error
				junctionInfo, err = os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				return nil
			}
			var barrier MutationBarrier
			if introduced == "before-scan" {
				_ = introduceJunction(context.Background(), types.DebrisInfo{})
			} else {
				barrier = introduceJunction
			}
			items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
			if err != nil || len(items) != 1 || items[0].Size != 5000 {
				t.Fatalf("scan = %+v, %v", items, err)
			}
			// Supply the inventory directly so execution must refuse independently
			// of eligibility, including a junction introduced after selection.
			attempted := false
			var output bytes.Buffer
			total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(context.Background(), items, barrier,
				&output, &output, func(outcome CleanupMutationOutcome) { attempted = attempted || outcome.MutationAttempted })
			if !errors.Is(err, ErrCacheLeafSymlink) || total != 0 || attempted {
				t.Errorf("refusal = total %d attempted %t err %v", total, attempted, err)
			}
			if !strings.Contains(output.String(), ErrCacheLeafSymlink.Error()) || strings.Contains(output.String(), "removed:") {
				t.Errorf("human output must report refusal: %s", output.String())
			}
			if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeIrregular == 0 || !os.SameFile(junctionInfo, info) {
				t.Errorf("refused junction changed: %v", err)
			}
			for _, dir := range []string{path, target} {
				if data, err := os.ReadFile(filepath.Join(dir, "payload")); err != nil || string(data) != payload {
					t.Errorf("payload changed at %q: %d bytes, %v", dir, len(data), err)
				}
			}
		})
	}
}
