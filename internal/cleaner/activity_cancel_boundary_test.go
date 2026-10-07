package cleaner

import (
	"context"
	"errors"
	"fmt"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecuteRefusesPostBarrierCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, "project", "node_modules")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var last CleanupMutationOutcome
	total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(ctx,
		[]types.DebrisInfo{{Path: path, Category: types.CategoryNodeModules, Tool: types.ToolNodeModules}}, nil, io.Discard, io.Discard,
		func(outcome CleanupMutationOutcome) { last = outcome; cancel() })
	if !errors.Is(err, context.Canceled) || total != 0 {
		t.Errorf("execution = %d, %v; want 0, canceled", total, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cancelled target lost: %v", err)
	}
	if last.MutationAttempted {
		t.Errorf("receipt claims mutation before canceled deletion: %+v", last)
	}
}

func TestExecuteRechecksMeasurementAndObserverDrift(t *testing.T) {
	for _, phase := range []string{"measurement", "observer"} {
		for _, drift := range []string{"identity", "activity", "cancel"} {
			t.Run(phase+"/"+drift, func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				path := filepath.Join(home, "project", "node_modules")
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(path, "payload")
				if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
				old := time.Now().Add(-72 * time.Hour)
				for _, p := range []string{path, file} {
					if err := os.Chtimes(p, old, old); err != nil {
						t.Fatal(err)
					}
				}
				item := types.DebrisInfo{Path: path, Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, ModTime: old, PathModTime: old}
				snapshot, err := CaptureCleanupTargetSnapshot(item, types.PruneOptions{Age: 24 * time.Hour})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				inject := func() {
					switch drift {
					case "identity":
						if err := os.Rename(path, path+"-original"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(path, 0o700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(file, []byte("replacement"), 0o600); err != nil {
							t.Fatal(err)
						}
						if err := os.Chtimes(path, old, old); err != nil {
							t.Fatal(err)
						}
					case "activity":
						if err := os.WriteFile(file, []byte("recent"), 0o600); err != nil {
							t.Fatal(err)
						}
					case "cancel":
						cancel()
					}
				}
				original := observedSize
				t.Cleanup(func() { observedSize = original })
				observedSize = func(ctx context.Context, p string) int64 {
					size := original(ctx, p)
					if phase == "measurement" {
						inject()
					}
					return size
				}
				var last CleanupMutationOutcome
				total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(ctx, []types.DebrisInfo{item},
					func(ctx context.Context, _ types.DebrisInfo) error { return snapshot.Validate(ctx) }, io.Discard, io.Discard,
					func(outcome CleanupMutationOutcome) {
						if phase == "observer" && !outcome.MutationAttempted && outcome.ResidualBytes == 0 {
							inject()
						}
						last = outcome
					})
				if err == nil || total != 0 || last.MutationAttempted {
					t.Errorf("drift approved: freed=%d attempted=%t err=%v", total, last.MutationAttempted, err)
				}
				if _, err := os.Stat(file); err != nil {
					t.Errorf("refused target lost: %v", err)
				}
			})
		}
	}
}

func TestObserveReclamationAfterPartialCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	for _, incomplete := range []bool{false, true} {
		t.Run(fmt.Sprint(incomplete), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cache")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"remove", "keep"} {
				if err := os.WriteFile(filepath.Join(path, name), []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := observedResidualSize
			t.Cleanup(func() { observedResidualSize = original })
			observedResidualSize = func(measure context.Context, p string) (int64, error) {
				if measure.Err() != nil {
					t.Error("residual context is already cancelled")
				}
				deadline, ok := measure.Deadline()
				if !ok || time.Until(deadline) > residualMeasurementTimeout {
					t.Error("residual measurement has no small deadline")
				}
				if incomplete {
					return 1, context.DeadlineExceeded
				}
				return original(measure, p)
			}
			freed, residual, attempted, err := observeReclamation(ctx, path, func() (bool, error) {
				if err := os.Remove(filepath.Join(path, "remove")); err != nil {
					t.Fatal(err)
				}
				cancel()
				return true, context.Canceled
			})
			wantFreed, wantResidual := int64(4), int64(4)
			if incomplete {
				wantFreed, wantResidual = 0, 8
			}
			if !errors.Is(err, context.Canceled) || !attempted || freed != wantFreed || residual != wantResidual {
				t.Errorf("partial cancellation: freed=%d residual=%d attempted=%t err=%v", freed, residual, attempted, err)
			}
			if _, err := os.Stat(filepath.Join(path, "keep")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExecuteRechecksCommandPreparationDrift(t *testing.T) {
	for _, drift := range []string{"identity", "activity", "cancel"} {
		t.Run(drift, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := testutil.GoBuildCache(home)
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(path, "keep")
			if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-72 * time.Hour)
			for _, p := range []string{path, file} {
				if err := os.Chtimes(p, old, old); err != nil {
					t.Fatal(err)
				}
			}
			item := types.DebrisInfo{Path: path, Category: types.CategoryBuildCache, Tool: types.ToolBuildCache, ID: "go-build", ModTime: old, PathModTime: old, CleanupKind: types.CleanupCommand, CleanupCommand: []string{"go", "clean", "-cache"}}
			snapshot, err := CaptureCleanupTargetSnapshot(item, types.PruneOptions{Age: 24 * time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := lookPath
			t.Cleanup(func() { lookPath = original })
			// A fake nonexistent binary cannot invoke a real package manager.
			lookPath = func(string) (string, error) {
				switch drift {
				case "cancel":
					cancel()
				case "identity":
					if err := os.Rename(path, path+"-original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte("replacement"), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(path, old, old); err != nil {
						t.Fatal(err)
					}
				case "activity":
					if err := os.WriteFile(file, []byte("recent"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				return filepath.Join(t.TempDir(), "not-a-real-go"), nil
			}
			var last CleanupMutationOutcome
			total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(ctx, []types.DebrisInfo{item}, func(ctx context.Context, _ types.DebrisInfo) error { return snapshot.Validate(ctx) }, io.Discard, io.Discard, func(outcome CleanupMutationOutcome) { last = outcome })
			if err == nil || total != 0 || last.MutationAttempted {
				t.Errorf("command preparation drift approved: freed=%d attempted=%t err=%v", total, last.MutationAttempted, err)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatal(err)
			}
		})
	}
}
