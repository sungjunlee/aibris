//go:build windows

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCleanupPathIdentityRejectsReparsePoint(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "reparse-target")
	if err := os.Symlink(target, link); err != nil {
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
		if output, junctionErr := command.CombinedOutput(); junctionErr != nil {
			t.Fatalf("creating Windows reparse-point fixture: symlink: %v; junction: %v\n%s",
				err, junctionErr, output)
		}
	}

	if _, err := platformCleanupPathIdentity(link); err == nil ||
		!strings.Contains(err.Error(), "reparse-point cleanup targets are not cacheable") {
		t.Fatalf("platformCleanupPathIdentity(%q) error = %v; want fail-closed reparse-point refusal",
			link, err)
	}
	if _, _, err := cleanupPathIdentity(link); err == nil ||
		!strings.Contains(err.Error(), "not cacheable") {
		t.Fatalf("cleanupPathIdentity(%q) error = %v; want fail-closed cached-target refusal",
			link, err)
	}
}
