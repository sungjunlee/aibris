//go:build windows

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func runPowerShellSnippet(t *testing.T, home, script string) string {
	t.Helper()
	out, err := runPowerShellSnippetExpectError(t, home, script)
	if err != nil {
		t.Fatalf("PowerShell script failed: %v\n%s", err, out)
	}
	return out
}

func runPowerShellSnippetExpectError(t *testing.T, home, script string) (string, error) {
	t.Helper()
	testutil.SetHome(t, home)
	temp := filepath.Join(home, "tmp")
	if err := os.MkdirAll(temp, 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	psCmd := "pwsh"
	if _, err := exec.LookPath(psCmd); err != nil {
		psCmd = "powershell.exe"
	}
	cmd := exec.CommandContext(ctx, psCmd, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Env = []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"PROCESSOR_ARCHITECTURE=AMD64",
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"TEMP=" + temp,
		"TMP=" + temp,
		"TMPDIR=" + temp,
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("PowerShell script timed out: %v\n%s", ctx.Err(), out)
	}
	return string(out), err
}

func powerShellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// TestNativeInstallPowerShellHelp verifies that install.ps1 shows usage
func TestNativeInstallPowerShellHelp(t *testing.T) {
	home := t.TempDir()
	output := runPowerShellSnippet(t, home, `
. .\install.ps1; Show-Usage
`)

	for _, want := range []string{
		"Install aibris.",
		"-Version",
		"-Prefix",
		"-AddToPath",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("help missing %q; output:\n%s", want, output)
		}
	}
}

// TestNativeInstallPowerShellDefaultDir verifies default install directory
func TestNativeInstallPowerShellDefaultDir(t *testing.T) {
	home := t.TempDir()
	output := runPowerShellSnippet(t, home, `
. .\install.ps1
Get-DefaultInstallDir
`)

	expected := filepath.Join(home, "AppData", "Local", "Programs", "aibris")
	if !strings.Contains(output, expected) {
		t.Errorf("default install dir = %q; want %q", strings.TrimSpace(output), expected)
	}
}

// TestNativeInstallPowerShellArchDetection verifies architecture detection
func TestNativeInstallPowerShellArchDetection(t *testing.T) {
	home := t.TempDir()
	output := runPowerShellSnippet(t, home, `
$env:PROCESSOR_ARCHITECTURE = "AMD64"
. .\install.ps1; Get-Architecture
`)

	if !strings.Contains(strings.TrimSpace(output), "amd64") {
		t.Errorf("arch detection = %q; want amd64", strings.TrimSpace(output))
	}
}

// TestNativeInstallPowerShellVersionNormalization verifies version tag normalization
func TestNativeInstallPowerShellVersionNormalization(t *testing.T) {
	home := t.TempDir()
	output := runPowerShellSnippet(t, home, `
. .\install.ps1
Normalize-Version -Ver "0.12.1"
Normalize-Version -Ver "v0.12.1"
`)

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines; got:\n%s", output)
	}

	if !strings.Contains(lines[0], "v0.12.1") {
		t.Errorf("normalize 0.12.1 = %q; want v0.12.1", strings.TrimSpace(lines[0]))
	}
	if !strings.Contains(lines[1], "v0.12.1") {
		t.Errorf("normalize v0.12.1 = %q; want v0.12.1", strings.TrimSpace(lines[1]))
	}
}

func (f installFixture) windowsRelease(t *testing.T, failure string) []byte {
	t.Helper()
	binary, err := os.ReadFile(cliContractBinary)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	name := "aibris.exe"
	if failure == "binary-missing" {
		name = "README"
	}
	entry, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := archive.Bytes()
	if failure == "malformed-archive" {
		data = []byte("not a zip archive")
	}
	asset := "aibris_windows_amd64.zip"
	writeInstallFile(t, filepath.Join(f.release, asset), data, 0644)
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if failure == "checksum-mismatch" {
		hash = strings.Repeat("0", 64)
	}
	checksums := hash + "  " + asset + "\n"
	if failure == "checksum-missing" {
		checksums = ""
	}
	writeInstallFile(t, filepath.Join(f.release, "checksums.txt"), []byte(checksums), 0644)
	return binary
}

func (f installFixture) runPowerShell(t *testing.T, failure string) (string, error) {
	t.Helper()
	// The same Install-Aibris entry point handles success and every failure.
	// Invoke-WebRequest is the only normal-path stub; files, hashes, extraction
	// and replacement use the production functions and native filesystem.
	script := `
. .\install.ps1
$release = ` + powerShellLiteral(f.release) + `
$prefix = ` + powerShellLiteral(f.prefix) + `
$failure = ` + powerShellLiteral(failure) + `
$existingDestination = Join-Path $prefix 'aibris.exe'
function Invoke-WebRequest {
    param($Uri, $OutFile, [switch]$UseBasicParsing, $TimeoutSec)
    if ($failure -eq 'download' -or ($failure -eq 'checksum-download' -and $Uri.EndsWith('/checksums.txt'))) {
        [IO.File]::WriteAllText($OutFile, 'partial download')
        throw 'injected download failure'
    }
    $asset = ([Uri]$Uri).Segments[-1]
    Microsoft.PowerShell.Management\Copy-Item -LiteralPath (Join-Path $release $asset) -Destination $OutFile
}
$lock = $null
try {
    if ($failure -eq 'locked') {
        $lock = [IO.File]::Open($existingDestination, 'Open', 'Read', 'Read')
    }
    if ($failure -eq 'stage-write') {
        function Copy-Item {
            param($Path, $Destination, [switch]$Force)
            [IO.File]::WriteAllText($Destination, 'partial binary')
            throw 'injected stage write failure'
        }
    }
    if ($failure -eq 'replacement') {
        function Copy-Item {
            param($Path, $Destination, [switch]$Force)
            Microsoft.PowerShell.Management\Copy-Item -Path $Path -Destination $Destination -Force:$Force
            # Introduce a native replacement failure after the stage is ready.
            [IO.File]::SetAttributes($existingDestination, [IO.FileAttributes]::ReadOnly)
            Write-Host 'injected read-only replacement failure'
        }
    }
    if ($failure -eq 'relative-prefix') {
        Set-Location (Split-Path -Parent $prefix)
        $prefix = Split-Path -Leaf $prefix
    }
    Install-Aibris -Version '0.12.1' -Prefix $prefix -Arch 'amd64'
}
catch {
    if ($failure -eq 'replacement') {
        $nativeError = $_.Exception.InnerException
        if ($nativeError -is [System.ComponentModel.Win32Exception]) {
            Write-Host "native-error=$($nativeError.NativeErrorCode)"
        }
    }
    throw
}
finally {
    if ($lock) { $lock.Dispose() }
    if (Test-Path $existingDestination) { [IO.File]::SetAttributes($existingDestination, [IO.FileAttributes]::Normal) }
}
`
	return runPowerShellSnippetExpectError(t, f.home, script)
}

func testPowerShellFailurePreservesExisting(t *testing.T, failure, message string) {
	t.Helper()
	f := newInstallFixture(t)
	f.windowsRelease(t, failure)
	existing := []byte("existing aibris binary\x00\xff")
	binary := filepath.Join(f.prefix, "aibris.exe")
	writeInstallFile(t, binary, existing, 0755)
	output, err := f.runPowerShell(t, failure)
	if err == nil {
		t.Errorf("installer succeeded on %s; output:\n%s", failure, output)
	} else if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("installer did not run: %v", err)
	}
	if !strings.Contains(output, message) {
		t.Errorf("failure did not reach expected boundary %q:\n%s", message, output)
	}
	if failure == "replacement" && !strings.Contains(output, "native-error=5") {
		t.Errorf("read-only replacement did not report native access-denied error:\n%s", output)
	}
	assertInstallContents(t, binary, existing)
	f.assertClean(t, "aibris.exe")
}

func TestNativeInstallPowerShellChecksumMismatchPreservesExisting(t *testing.T) {
	testPowerShellFailurePreservesExisting(t, "checksum-mismatch", "SHA-256 checksum mismatch")
}

func TestNativeInstallPowerShellLockedBinaryPreserved(t *testing.T) {
	testPowerShellFailurePreservesExisting(t, "locked", "locked or in use")
}

func TestNativeInstallPowerShellReleaseFailuresPreserveExisting(t *testing.T) {
	for _, tc := range []struct{ failure, message string }{
		{"download", "Failed to download release"},
		{"checksum-download", "Failed to download release"},
		{"checksum-missing", "not found in checksums.txt"},
		{"malformed-archive", "Checksum verified"},
		{"binary-missing", "aibris.exe not found in archive"},
		{"stage-write", "injected stage write failure"},
		{"replacement", "injected read-only replacement failure"},
	} {
		t.Run(tc.failure, func(t *testing.T) {
			testPowerShellFailurePreservesExisting(t, tc.failure, tc.message)
		})
	}
}

func TestNativeInstallPowerShellSameVersionRerun(t *testing.T) {
	f := newInstallFixture(t)
	binary := f.windowsRelease(t, "")
	for i := 0; i < 2; i++ {
		output, err := f.runPowerShell(t, "")
		if err != nil {
			t.Fatalf("install %d failed: %v\n%s", i+1, err, output)
		}
		assertInstallContents(t, filepath.Join(f.prefix, "aibris.exe"), binary)
		f.assertClean(t, "aibris.exe")
	}
}

func TestNativeInstallPowerShellEmptyPrefixFirstInstall(t *testing.T) {
	f := newInstallFixture(t)
	binary := f.windowsRelease(t, "")
	// Exercise creation of a destination directory too, beyond an empty prefix.
	parent := f.prefix
	f.prefix = filepath.Join(parent, "new-install")
	output, err := f.runPowerShell(t, "relative-prefix")
	if err != nil {
		t.Fatalf("first install failed: %v\n%s", err, output)
	}
	assertInstallContents(t, filepath.Join(f.prefix, "aibris.exe"), binary)
	assertInstallContents(t, filepath.Join(parent, "sentinel"), []byte("keep parent"))
	writeInstallFile(t, filepath.Join(f.prefix, "sentinel"), []byte("keep parent"), 0644)
	f.assertClean(t, "aibris.exe")
}

// TestNativeInstallPowerShellPathHintWithoutAddToPath verifies that the installer
// shows PATH guidance when -AddToPath is not used
func TestNativeInstallPowerShellPathHintWithoutAddToPath(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, "bin")

	script := `
$ErrorActionPreference = "Stop"
. .\install.ps1
$script:Binary = "aibris.exe"
Show-PathHint -InstallDir "` + installDir + `"
`

	output := runPowerShellSnippet(t, home, script)

	for _, want := range []string{
		"aibris.exe was installed",
		"not on your PATH yet",
		"Add it for future PowerShell sessions",
		"Use it in this shell now",
		"$env:Path",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("PATH hint missing %q; output:\n%s", want, output)
		}
	}
}

// TestNativeInstallPowerShellPathHintSkipsWhenOnPath verifies that PATH hint
// is not shown when install directory is already on PATH
func TestNativeInstallPowerShellPathHintSkipsWhenOnPath(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, "bin")

	script := `
$ErrorActionPreference = "Stop"
$env:Path = "` + installDir + `;$env:Path"
. .\install.ps1
$script:Binary = "aibris.exe"
Show-PathHint -InstallDir "` + installDir + `"
`

	output := runPowerShellSnippet(t, home, script)

	if strings.Contains(output, "not on your PATH yet") {
		t.Errorf("PATH hint should not be shown when directory is on PATH; output:\n%s", output)
	}
}
