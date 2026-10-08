package cleaner

import (
	"context"
	"errors"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotRefusesCancelledActivityEvidence(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, "cache")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureCleanupTargetSnapshot(types.DebrisInfo{Path: path, ModTime: old, PathModTime: old}, types.PruneOptions{Age: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshot.Validate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate = %v; want cancellation refusal", err)
	}
}

func TestSnapshotRefusesIncompleteActivityEvidence(t *testing.T) {
	for _, fault := range []error{os.ErrPermission, errors.New("traversal I/O error"), context.Canceled} {
		t.Run(fault.Error(), func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, "cache")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-72 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			snapshot, err := CaptureCleanupTargetSnapshot(types.DebrisInfo{Path: path, ModTime: old, PathModTime: old}, types.PruneOptions{Age: 24 * time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			original := completeTreeModTime
			t.Cleanup(func() { completeTreeModTime = original })
			completeTreeModTime = func(context.Context, string) (time.Time, error) { return old, fault }
			if err := snapshot.Validate(context.Background()); !errors.Is(err, fault) {
				t.Fatalf("Validate = %v; want incomplete evidence refusal %v", err, fault)
			}
		})
	}
}

func TestSnapshotRechecksAfterActivityObservation(t *testing.T) {
	for _, drift := range []string{"cancel", "identity"} {
		t.Run(drift, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, "cache")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-72 * time.Hour)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
			snapshot, err := CaptureCleanupTargetSnapshot(types.DebrisInfo{Path: path, ModTime: old, PathModTime: old}, types.PruneOptions{Age: 24 * time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			original := completeTreeModTime
			t.Cleanup(func() { completeTreeModTime = original })
			completeTreeModTime = func(context.Context, string) (time.Time, error) {
				if drift == "cancel" {
					cancel()
				} else {
					if err := os.Rename(path, path+"-original"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(path, old, old); err != nil {
						t.Fatal(err)
					}
				}
				return old, nil
			}
			if err := snapshot.Validate(ctx); err == nil {
				t.Fatal("drift during activity walk approved")
			}
		})
	}
}
