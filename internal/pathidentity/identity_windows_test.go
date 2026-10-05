//go:build windows

package pathidentity

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWindowsCleanupPathIdentityRejectsReparsePoint(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Run("junction", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "junction-target")
		command := exec.Command(
			"powershell.exe",
			"-NoLogo",
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			`$ErrorActionPreference = 'Stop'; New-Item -ItemType Junction -Path $env:AIBRIS_TEST_LINK -Target $env:AIBRIS_TEST_TARGET | Out-Null`,
		)
		command.Env = append(
			os.Environ(),
			"AIBRIS_TEST_LINK="+link,
			"AIBRIS_TEST_TARGET="+target,
		)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("creating Windows junction fixture: %v\n%s", err, output)
		}

		if _, err := platformCleanupPathIdentity(link); err == nil ||
			!strings.Contains(err.Error(), "reparse-point cleanup targets are not cacheable") {
			t.Fatalf("platformCleanupPathIdentity(%q) error = %v; want fail-closed reparse-point refusal",
				link, err)
		}
		if _, _, err := PathIdentity(link); err == nil ||
			!strings.Contains(err.Error(), "reparse-point cleanup targets are not cacheable") {
			t.Fatalf("PathIdentity(%q) error = %v; want fail-closed reparse-point refusal",
				link, err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "symlink-target")
		if err := os.Symlink(target, link); err != nil {
			if errors.Is(err, syscall.ERROR_PRIVILEGE_NOT_HELD) {
				t.Skipf("creating Windows symlink fixture requires symlink privilege: %v", err)
			}
			t.Fatal(err)
		}

		if _, err := platformCleanupPathIdentity(link); err == nil ||
			!strings.Contains(err.Error(), "reparse-point cleanup targets are not cacheable") {
			t.Fatalf("platformCleanupPathIdentity(%q) error = %v; want fail-closed reparse-point refusal",
				link, err)
		}
		if _, _, err := PathIdentity(link); err == nil ||
			!strings.Contains(err.Error(), "symbolic-link cleanup targets are not cacheable") {
			t.Fatalf("PathIdentity(%q) error = %v; want fail-closed symbolic-link refusal",
				link, err)
		}
	})
}
