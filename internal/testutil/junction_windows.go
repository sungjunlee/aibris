//go:build windows

package testutil

import (
	"os"
	"os/exec"
	"testing"
)

// WindowsJunction creates a native junction without requiring symlink privilege.
// Fixture creation failures are fatal so Windows CI cannot silently skip coverage.
func WindowsJunction(tb testing.TB, link, target string) {
	tb.Helper()
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
		`$ErrorActionPreference = 'Stop'; New-Item -ItemType Junction -Path $env:AIBRIS_TEST_LINK -Target $env:AIBRIS_TEST_TARGET | Out-Null`)
	command.Env = append(os.Environ(), "AIBRIS_TEST_LINK="+link, "AIBRIS_TEST_TARGET="+target)
	if output, err := command.CombinedOutput(); err != nil {
		tb.Fatalf("creating Windows junction fixture: %v\n%s", err, output)
	}
	info, err := os.Lstat(link)
	if err != nil {
		tb.Fatal(err)
	}
	if info.Mode()&os.ModeIrregular == 0 || info.Mode()&os.ModeSymlink != 0 {
		tb.Fatalf("junction mode = %v; want a non-symlink reparse point", info.Mode())
	}
}
