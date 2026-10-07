package adapter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func sparseSizeFile(t testing.TB, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A local PATH controls availability, exit status, and output without relying
// on the host's du or filesystem allocation. The success stub represents a
// valid allocated-byte response; it deliberately differs from apparent size.
func sizeTestDU(t *testing.T, mode string) {
	t.Helper()
	bin := t.TempDir()
	if mode != "missing" {
		if runtime.GOOS == "windows" {
			t.Skip("shell du fixtures require Unix")
		}
		body := "exit 1\n"
		switch mode {
		case "success":
			body = "shift\nfor p do printf '0\\t%s\\n' \"$p\"; done\n"
		case "malformed":
			body = "printf 'unparseable\\n'\n"
		}
		if err := os.WriteFile(filepath.Join(bin, "du"), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

func TestEstimateDirSizesApparentBytesIndependentOfDU(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root, empty := filepath.Join(home, "tree with spaces"), filepath.Join(home, "empty")
	for _, p := range []string{root, empty} {
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const size = int64(1 << 30)
	sparseSizeFile(t, filepath.Join(root, "sparse"), size)
	paths := []string{root, empty}
	for _, mode := range []string{"success", "missing", "failure", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			sizeTestDU(t, mode)
			got := estimateDirSizes(context.Background(), paths)
			if len(got) != 2 || got[root] != size || got[empty] != 0 {
				t.Fatalf("sizes = %v; want sparse apparent %d and empty 0", got, size)
			}
			activity := estimateDirActivity(context.Background(), root)
			if activity.Err != nil || activity.Size != got[root] {
				t.Fatalf("activity = %+v; batched size = %d", activity, got[root])
			}
		})
	}
}

func TestEstimateDirSizesHardlinksCountEachPath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	sizeTestDU(t, "success")
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	for _, p := range []string{a, b, filepath.Join(a, "nested")} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(a, "payload")
	sparseSizeFile(t, source, 12345)
	for _, p := range []string{filepath.Join(a, "nested", "link"), filepath.Join(b, "link")} {
		if err := os.Link(source, p); err != nil {
			t.Skipf("hardlinks unavailable: %v", err)
		}
	}
	ctx := context.Background()
	got := estimateDirSizes(ctx, []string{a, b})
	if got[a] != 24690 || got[b] != 12345 {
		t.Fatalf("hardlink sizes = %v; want per-path lengths, independently per target", got)
	}
	for _, p := range []string{a, b} {
		if activity := estimateDirActivity(ctx, p); activity.Err != nil || activity.Size != got[p] {
			t.Fatalf("activity = %+v; size = %d", activity, got[p])
		}
	}
}

func TestEstimateDirSizesNestedSymlinksNeverFollowTargets(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	sizeTestDU(t, "missing")
	root, outside := filepath.Join(home, "tree"), filepath.Join(home, "outside")
	for _, p := range []string{root, outside, filepath.Join(root, "nested")} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(outside, "payload")
	sparseSizeFile(t, file, 1<<30)
	links := map[string]string{
		filepath.Join(root, "file-link"):          file,
		filepath.Join(root, "nested", "dir-link"): outside,
		filepath.Join(root, "dangling"):           filepath.Join(home, "missing"),
	}
	var want int64
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatal(err)
		}
		want += info.Size()
	}
	ctx := context.Background()
	got := estimateDirSizes(ctx, []string{root})
	if got[root] != want {
		t.Errorf("tree size = %d; want link lengths %d", got[root], want)
	}
	if activity := estimateDirActivity(ctx, root); activity.Err != nil || activity.Size != want {
		t.Errorf("tree activity = %+v; want link lengths %d", activity, want)
	}
}

func TestEstimateDirSizesRootSymlinkFollowsTarget(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, ".gradle", "caches")
	target := filepath.Join(home, "relocated-cache")
	nested := filepath.Join(target, "nested")
	for _, path := range []string{filepath.Dir(root), nested} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(nested, "payload")
	const wantSize = int64(5000)
	sparseSizeFile(t, file, wantSize)
	if err := os.Symlink(target, root); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	linkInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	// Make the link older than the payload without platform-specific link
	// timestamp setters or sleeps. Directory mtimes must not mask the payload.
	recent := linkInfo.ModTime().Add(24 * time.Hour).Truncate(time.Second)
	old := recent.Add(-30 * 24 * time.Hour)
	for _, path := range []string{target, nested} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(file, recent, recent); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if got := estimateDirSizes(ctx, []string{root})[root]; got != wantSize {
		t.Errorf("root symlink size = %d; want tree size %d", got, wantSize)
	}
	if activity := estimateDirActivity(ctx, root); activity.Err != nil || activity.Size != wantSize || !activity.NewestModTime.Equal(recent) {
		t.Errorf("root symlink activity = %+v; want size %d and mtime %v", activity, wantSize, recent)
	}
	items, err := (&BuildCacheAdapter{}).Scan(ctx, types.ScanOptions{})
	if err != nil || len(items) != 1 {
		t.Fatalf("cache scan = %v, %v; want one relocated cache", items, err)
	}
	if got := items[0]; got.Path != root || got.Size != wantSize || !got.ModTime.Equal(recent) || !got.PathModTime.Equal(old) {
		t.Errorf("cache scan = %+v; want tree size %d, activity %v, path mtime %v", got, wantSize, recent, old)
	}
	if got, err := CompleteTreeModTime(ctx, root); err != nil || !got.Equal(recent) {
		t.Errorf("complete root activity = %v, %v; want %v", got, err, recent)
	}
	// Mutation preflight must observe activity newer than the scan snapshot.
	newer := recent.Add(time.Hour)
	if err := os.Chtimes(file, newer, newer); err != nil {
		t.Fatal(err)
	}
	if got, err := CompleteTreeModTime(ctx, root); err != nil || !got.Equal(newer) {
		t.Errorf("rechecked root activity = %v, %v; want %v", got, err, newer)
	}
	if err := os.Rename(target, target+"-moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteTreeModTime(ctx, root); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing root target activity error = %v; want not-exist", err)
	}
}

func TestSizeObservationKeepsActivityErrorWithDUAvailable(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	sizeTestDU(t, "success")
	root := filepath.Join(home, "tree")
	if err := os.MkdirAll(filepath.Join(root, "unreadable"), 0o700); err != nil {
		t.Fatal(err)
	}
	sparseSizeFile(t, filepath.Join(root, "readable"), 123)
	original := walkDirectory
	t.Cleanup(func() { walkDirectory = original })
	walkDirectory = func(path string, visit fs.WalkDirFunc) error { return visit(path, nil, fs.ErrPermission) }
	ctx := context.Background()
	if size := estimateDirSizes(ctx, []string{root})[root]; size != 123 {
		t.Errorf("report-only bytes = %d; want readable 123", size)
	}
	if activity := estimateDirActivity(ctx, root); !errors.Is(activity.Err, fs.ErrPermission) || activity.Size != 123 {
		t.Errorf("activity = %+v; want partial bytes and observation error", activity)
	}
	if _, err := CompleteTreeModTime(ctx, root); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("safety observation error lost: %v", err)
	}
}

func TestWorktreeAndStripApparentSizes(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	sizeTestDU(t, "success")
	checkout := filepath.Join(home, "worktrees", "unit")
	createWorktreeGit(t, checkout, filepath.Join(home, "repo"), "unit")
	if err := os.WriteFile(filepath.Join(checkout, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := filepath.Join(checkout, "node_modules")
	if err := os.Mkdir(deps, 0o700); err != nil {
		t.Fatal(err)
	}
	const payload = int64(1 << 30)
	sparseSizeFile(t, filepath.Join(deps, "payload"), payload)
	marker, err := os.Lstat(filepath.Join(checkout, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := NewWorktreeAdapter().Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %v, %v", items, err)
	}
	if got := items[0]; got.Size != payload+2+marker.Size() || got.StrippableBytes != payload {
		t.Fatalf("worktree size = %d, strip = %d; want apparent owner %d, strip %d", got.Size, got.StrippableBytes, payload+2+marker.Size(), payload)
	}
}

func BenchmarkEstimateDir(b *testing.B) {
	for _, shape := range []string{"flat", "wide", "deep"} {
		b.Run(shape, func(b *testing.B) {
			home := b.TempDir()
			testutil.SetHome(b, home)
			root := filepath.Join(home, "tree")
			dir := root
			for i := 0; i < 256; i++ {
				switch shape {
				case "wide":
					dir = filepath.Join(root, fmt.Sprint(i%16))
				case "deep":
					if i%16 == 0 {
						dir = filepath.Join(dir, "level")
					}
				}
				if err := os.MkdirAll(dir, 0o700); err != nil {
					b.Fatal(err)
				}
				sparseSizeFile(b, filepath.Join(dir, fmt.Sprint(i)), 4096)
			}
			paths := []string{root}
			for _, estimator := range []string{"walker", "activity", "batch", "batch-no-du"} {
				b.Run(estimator, func(b *testing.B) {
					if estimator == "batch-no-du" {
						b.Setenv("PATH", b.TempDir())
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						switch estimator {
						case "walker":
							estimateDirSize(context.Background(), root)
						case "activity":
							estimateDirActivity(context.Background(), root)
						default:
							estimateDirSizes(context.Background(), paths)
						}
					}
				})
			}
		})
	}
}
