package adapter

import (
	"os"
	"path/filepath"
	"testing"
)

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
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	os.WriteFile(f1, make([]byte, 100), 0644)
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f2, make([]byte, 200), 0644)

	got := estimateDirSize(dir)
	want := int64(300)
	if got != want {
		t.Errorf("estimateDirSize(%q) = %d; want %d", dir, got, want)
	}
}

func TestEstimateDirSize_Empty(t *testing.T) {
	dir := t.TempDir()
	got := estimateDirSize(dir)
	if got != 0 {
		t.Errorf("estimateDirSize() = %d; want 0", got)
	}
}

func TestEstimateDirSize_Nested(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subdir")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, "a.txt"), make([]byte, 150), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), make([]byte, 50), 0644)

	got := estimateDirSize(dir)
	want := int64(200)
	if got != want {
		t.Errorf("estimateDirSize(nested) = %d; want %d", got, want)
	}
}

func TestEstimateDirSize_NonExistent(t *testing.T) {
	got := estimateDirSize("/nonexistent-path-xyzzy")
	if got != 0 {
		t.Errorf("estimateDirSize(non-existent) = %d; want 0", got)
	}
}
