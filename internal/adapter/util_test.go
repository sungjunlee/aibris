package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setTestModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("os.Chtimes(%q): %v", path, err)
	}
}

func TestDetectProjectName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0755)
	os.MkdirAll(filepath.Join(dir, "myproject"), 0755)

	got := detectProjectName(dir)
	want := "myproject"
	if got != want {
		t.Errorf("detectProjectName(%q) = %q; want %q", dir, got, want)
	}
}

func TestDetectProjectName_NoVisible(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0755)

	got := detectProjectName(dir)
	if got != "" {
		t.Errorf("detectProjectName(%q) = %q; want empty", dir, got)
	}
}

func TestDetectProjectName_OnlyFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), nil, 0644)

	got := detectProjectName(dir)
	if got != "" {
		t.Errorf("detectProjectName with only files = %q; want empty", got)
	}
}

func TestDetectProjectName_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got := detectProjectName(dir)
	if got != "" {
		t.Errorf("detectProjectName with empty dir = %q; want empty", got)
	}
}

func TestDetectProjectName_NonExistent(t *testing.T) {
	got := detectProjectName("/nonexistent-path-xyzzy")
	if got != "" {
		t.Errorf("detectProjectName with non-existent = %q; want empty", got)
	}
}

func TestDetectProjectName_FirstNonHiddenWins(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "alpha"), 0755)
	os.MkdirAll(filepath.Join(dir, "beta"), 0755)

	got := detectProjectName(dir)
	if got != "alpha" {
		t.Errorf("detectProjectName = %q; want 'alpha' (first non-hidden)", got)
	}
}

func TestIsHiddenDir(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{".hidden", true},
		{"visible", false},
		{"", false},
		{".foo.bar", true},
	}
	for _, tt := range tests {
		if got := isHiddenDir(tt.name); got != tt.want {
			t.Errorf("isHiddenDir(%q) = %v; want %v", tt.name, got, tt.want)
		}
	}
}

func TestEstimateDirSize(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	os.WriteFile(f1, make([]byte, 100), 0644)
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f2, make([]byte, 200), 0644)

	got := estimateDirSize(ctx, dir)
	want := int64(300)
	if got != want {
		t.Errorf("estimateDirSize(%q) = %d; want %d", dir, got, want)
	}
}

func TestEstimateDirSize_Empty(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	got := estimateDirSize(ctx, dir)
	if got != 0 {
		t.Errorf("estimateDirSize() = %d; want 0", got)
	}
}

func TestEstimateDirSize_Nested(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "a.txt"), make([]byte, 150), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), make([]byte, 50), 0644)

	got := estimateDirSize(ctx, dir)
	want := int64(200)
	if got != want {
		t.Errorf("estimateDirSize(nested) = %d; want %d", got, want)
	}
}

func TestEstimateDirSize_NonExistent(t *testing.T) {
	ctx := context.Background()
	got := estimateDirSize(ctx, "/nonexistent-path-xyzzy")
	if got != 0 {
		t.Errorf("estimateDirSize(non-existent) = %d; want 0", got)
	}
}

func TestEstimateDirActivity_NestedNewestModTime(t *testing.T) {
	dir := t.TempDir()
	deep := filepath.Join(dir, "one", "two", "three")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "cache.bin")
	if err := os.WriteFile(file, []byte("cache"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	setTestModTime(t, dir, old)
	setTestModTime(t, filepath.Join(dir, "one"), old)
	setTestModTime(t, filepath.Join(dir, "one", "two"), old)
	setTestModTime(t, deep, old)
	setTestModTime(t, file, recent)
	expected := fileModTime(t, file)

	activity := estimateDirActivity(context.Background(), dir)
	if !activity.NewestModTime.Equal(expected) {
		t.Errorf("NewestModTime = %v; want nested file mtime %v", activity.NewestModTime, expected)
	}
}

func TestEstimateDirActivity_DirectoryModTime(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(subdir, "artifact.bin")
	if err := os.WriteFile(file, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	setTestModTime(t, dir, old)
	setTestModTime(t, file, old)
	setTestModTime(t, subdir, recent)
	expected := fileModTime(t, subdir)

	activity := estimateDirActivity(context.Background(), dir)
	if !activity.NewestModTime.Equal(expected) {
		t.Errorf("NewestModTime = %v; want directory mtime %v", activity.NewestModTime, expected)
	}
}

func TestEstimateDirActivity_EmptyDir(t *testing.T) {
	activity := estimateDirActivity(context.Background(), t.TempDir())
	if !activity.NewestModTime.IsZero() {
		t.Errorf("NewestModTime = %v; want zero", activity.NewestModTime)
	}
}

func TestEstimateDirActivity_SizeMatchesEstimateDirSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested", "deeper"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.bin"), make([]byte, 40), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "deeper", "leaf.bin"), make([]byte, 260), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	activity := estimateDirActivity(ctx, dir)
	if activity.Size != 300 {
		t.Errorf("estimateDirActivity().Size = %d; want 300", activity.Size)
	}
	if activity.Size != estimateDirSize(ctx, dir) {
		t.Errorf("activity size = %d; estimateDirSize = %d", activity.Size, estimateDirSize(ctx, dir))
	}
}

func TestEstimateDirActivity_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	activity := estimateDirActivity(ctx, t.TempDir())
	if activity.Size != 0 || !activity.NewestModTime.IsZero() {
		t.Errorf("estimateDirActivity(cancelled) = %+v; want zero activity", activity)
	}
}

func TestEstimateDirActivity_TopLevelFileModTime(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(subdir, "old.bin")
	if err := os.WriteFile(nested, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	topLevel := filepath.Join(dir, "recent.bin")
	if err := os.WriteFile(topLevel, []byte("recent"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	setTestModTime(t, nested, old)
	setTestModTime(t, subdir, old)
	setTestModTime(t, dir, old)
	setTestModTime(t, topLevel, recent)
	expected := fileModTime(t, topLevel)

	activity := estimateDirActivity(context.Background(), dir)
	if !activity.NewestModTime.Equal(expected) {
		t.Errorf("NewestModTime = %v; want top-level file mtime %v", activity.NewestModTime, expected)
	}
}

// TestEstimateDirActivity_ConcurrentSubdirWorkers gives the walk more
// top-level subdirectories than the worker pool has slots, so several worker
// goroutines record modification times at once. Single-subdirectory fixtures
// never let `go test -race` observe the accumulator's mutex.
func TestEstimateDirActivity_ConcurrentSubdirWorkers(t *testing.T) {
	const subdirs = 16
	dir := t.TempDir()
	base := time.Now().Add(-30 * 24 * time.Hour)
	newest := base
	for i := 0; i < subdirs; i++ {
		subdir := filepath.Join(dir, fmt.Sprintf("shard-%02d", i))
		if err := os.MkdirAll(subdir, 0755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(subdir, "entry.bin")
		if err := os.WriteFile(file, []byte("entry"), 0644); err != nil {
			t.Fatal(err)
		}
		modTime := base.Add(time.Duration(i) * time.Hour)
		setTestModTime(t, file, modTime)
		setTestModTime(t, subdir, base)
		if modTime.After(newest) {
			newest = modTime
		}
	}
	setTestModTime(t, dir, base)
	expected := fileModTime(t, filepath.Join(dir, fmt.Sprintf("shard-%02d", subdirs-1), "entry.bin"))
	if !expected.Equal(newest) {
		t.Fatalf("fixture newest mtime = %v; want %v", expected, newest)
	}

	activity := estimateDirActivity(context.Background(), dir)
	if !activity.NewestModTime.Equal(expected) {
		t.Errorf("NewestModTime = %v; want newest shard mtime %v", activity.NewestModTime, expected)
	}
	if activity.Size != int64(subdirs*len("entry")) {
		t.Errorf("Size = %d; want %d", activity.Size, subdirs*len("entry"))
	}
}

func fileModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}
