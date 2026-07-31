//go:build windows

package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordedCWDVolumeIDUsesWindowsVolumePath(t *testing.T) {
	home := t.TempDir()
	child := filepath.Join(home, "workspace")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	childInfo, err := os.Lstat(child)
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(child)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		t.Fatal(err)
	}

	childVolume, err := recordedCWDVolumeID(child, childInfo)
	if err != nil {
		t.Fatalf("recordedCWDVolumeID(child): %v", err)
	}
	parentVolume, err := recordedCWDVolumeID(parent, parentInfo)
	if err != nil {
		t.Fatalf("recordedCWDVolumeID(parent): %v", err)
	}
	if childVolume == "" {
		t.Fatal("recordedCWDVolumeID(child) returned an empty volume path")
	}
	if childVolume != strings.ToLower(filepath.Clean(childVolume)) {
		t.Fatalf("child volume ID = %q; want normalized lowercase path", childVolume)
	}
	if childVolume != parentVolume {
		t.Fatalf("ordinary child volume = %q, parent volume = %q; want equal", childVolume, parentVolume)
	}
}
