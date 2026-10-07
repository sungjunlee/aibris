package adapter

import (
	"context"
	"errors"
	"github.com/sungjunlee/aibris/internal/testutil"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestDirActivityRefusesIncompleteTraversal(t *testing.T) {
	for _, fault := range []error{fs.ErrPermission, errors.New("traversal I/O error"), fs.ErrNotExist, context.Canceled} {
		t.Run(fault.Error(), func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			root := filepath.Join(home, "cache")
			hidden := filepath.Join(root, "hidden")
			if err := os.MkdirAll(hidden, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "visible"), []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hidden, "recent"), []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			original := walkDirectory
			t.Cleanup(func() { walkDirectory = original })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			walkDirectory = func(path string, visit fs.WalkDirFunc) error {
				if fault == context.Canceled {
					cancel()
					return original(path, visit)
				}
				return visit(path, nil, fault)
			}
			got := estimateDirActivity(ctx, root)
			if !errors.Is(got.Err, fault) {
				t.Errorf("activity error = %v; want %v", got.Err, fault)
			}
			if fault != context.Canceled && estimateDirSize(context.Background(), root) != 3 {
				t.Error("report-only size should retain readable bytes")
			}
		})
	}
}

type unreadableInfoEntry struct{ fs.DirEntry }

func (e unreadableInfoEntry) Info() (fs.FileInfo, error) { return nil, fs.ErrPermission }

func TestDirActivityRefusesEntryInfoError(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	nested := filepath.Join(home, "cache", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(nested, "recent")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := walkDirectory
	t.Cleanup(func() { walkDirectory = original })
	walkDirectory = func(path string, visit fs.WalkDirFunc) error {
		return original(path, func(p string, d fs.DirEntry, err error) error {
			if p == file && err == nil {
				d = unreadableInfoEntry{d}
			}
			return visit(p, d, err)
		})
	}
	if _, err := CompleteTreeModTime(context.Background(), filepath.Dir(nested)); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("entry Info failure lost: %v", err)
	}
}

func TestCompleteActivityAndApproximateSizeContracts(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	missing := filepath.Join(home, "missing")
	if _, err := CompleteTreeModTime(context.Background(), missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing path activity=%v", err)
	}
	if size := EstimateDirSize(context.Background(), missing); size != 0 {
		t.Errorf("report-only missing size=%d", size)
	}
	if _, err := CompleteTreeModTime(context.Background(), home); err != nil {
		t.Fatalf("complete empty tree refused: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompleteTreeModTime(ctx, home); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error lost: %v", err)
	}
}
