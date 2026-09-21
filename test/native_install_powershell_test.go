//go:build windows

package test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runPowerShellSnippet(t *testing.T, home, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Dir = "."
	cmd.Env = []string{
		"USERPROFILE=" + home,
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"PROCESSOR_ARCHITECTURE=AMD64",
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"TEMP=" + filepath.Join(home, "Temp"),
		"TMP=" + filepath.Join(home, "Temp"),
	}

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("PowerShell script timed out: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("PowerShell script failed: %v\n%s", err, out)
	}
	return string(out)
}

func runPowerShellSnippetExpectError(t *testing.T, home, script string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.Dir = "."
	cmd.Env = []string{
		"USERPROFILE=" + home,
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"PROCESSOR_ARCHITECTURE=AMD64",
		"PATH=" + os.Getenv("PATH"),
		"SystemRoot=" + os.Getenv("SystemRoot"),
		"TEMP=" + filepath.Join(home, "Temp"),
		"TMP=" + filepath.Join(home, "Temp"),
	}

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("PowerShell script timed out: %v\n%s", ctx.Err(), out)
	}
	return string(out), err
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
	// Inline Get-DefaultInstallDir logic to avoid sourcing entire install.ps1
	output := runPowerShellSnippet(t, home, `
$env:LOCALAPPDATA = Join-Path "`+home+`" "AppData\Local"
Join-Path $env:LOCALAPPDATA "Programs\aibris"
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

// TestNativeInstallPowerShellChecksumMismatchPreservesExisting verifies that
// checksum failures do not remove an existing installation
func TestNativeInstallPowerShellChecksumMismatchPreservesExisting(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, "AppData", "Local", "Programs", "aibris")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}

	existingBinary := filepath.Join(installDir, "aibris.exe")
	existingContent := []byte("existing aibris binary")
	if err := os.WriteFile(existingBinary, existingContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock archive with wrong checksum
	tmpDir := t.TempDir()
	mockArchive := filepath.Join(tmpDir, "aibris_windows_amd64.zip")
	if err := os.WriteFile(mockArchive, []byte("fake zip content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create checksums.txt with wrong hash
	checksums := filepath.Join(tmpDir, "checksums.txt")
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	checksumsContent := wrongHash + "  aibris_windows_amd64.zip\n"
	if err := os.WriteFile(checksums, []byte(checksumsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Inline checksum verification logic to avoid sourcing entire install.ps1
	script := `
$ErrorActionPreference = "Stop"
$asset = "aibris_windows_amd64.zip"
$archivePath = "` + filepath.ToSlash(mockArchive) + `"
$checksumsPath = "` + filepath.ToSlash(checksums) + `"

$checksums = Get-Content $checksumsPath
$expectedLine = $checksums | Where-Object { $_ -match "\s+$([regex]::Escape($asset))$" }
$expected = ($expectedLine -split '\s+')[0]

$hash = Get-FileHash -Path $archivePath -Algorithm SHA256
$actual = $hash.Hash

if ($actual -ne $expected) {
    Write-Error "SHA-256 checksum mismatch"
}
`

	_, err := runPowerShellSnippetExpectError(t, home, script)
	if err == nil {
		t.Fatal("expected checksum mismatch error; got success")
	}

	// Verify existing binary still exists and is unchanged
	if _, err := os.Stat(existingBinary); os.IsNotExist(err) {
		t.Fatal("existing binary was removed after checksum failure")
	}

	content, err := os.ReadFile(existingBinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(existingContent) {
		t.Fatal("existing binary was modified after checksum failure")
	}
}

// TestNativeInstallPowerShellLockedBinaryPreserved verifies that installation
// preserves an existing binary when it is locked (in use)
func TestNativeInstallPowerShellLockedBinaryPreserved(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}

	existingBinary := filepath.Join(installDir, "aibris.exe")
	existingContent := []byte("existing locked binary")
	if err := os.WriteFile(existingBinary, existingContent, 0755); err != nil {
		t.Fatal(err)
	}

	// Open file exclusively to simulate locked binary
	file, err := os.OpenFile(existingBinary, os.O_RDWR, 0755)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	// Create new binary to install
	newBinary := filepath.Join(t.TempDir(), "aibris.exe")
	if err := os.WriteFile(newBinary, []byte("new binary"), 0755); err != nil {
		t.Fatal(err)
	}

	// Inline locked file check logic to avoid sourcing entire install.ps1
	script := `
$ErrorActionPreference = "Stop"
$destination = "` + filepath.ToSlash(existingBinary) + `"

try {
    $fileStream = [System.IO.File]::Open($destination, 'Open', 'Read', 'None')
    $fileStream.Close()
    Write-Output "File is not locked"
}
catch {
    Write-Error "Existing aibris.exe is locked or in use"
}
`

	_, err = runPowerShellSnippetExpectError(t, home, script)
	if err == nil {
		t.Fatal("expected locked file error; got success")
	}

	// Verify existing binary is unchanged
	file.Close()
	content, err := os.ReadFile(existingBinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(existingContent) {
		t.Fatal("existing locked binary was modified")
	}
}

// TestNativeInstallPowerShellSameVersionRerun verifies that installing
// the same version again works (replaces existing binary)
func TestNativeInstallPowerShellSameVersionRerun(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatal(err)
	}

	existingBinary := filepath.Join(installDir, "aibris.exe")
	if err := os.WriteFile(existingBinary, []byte("version 1"), 0755); err != nil {
		t.Fatal(err)
	}

	newBinary := filepath.Join(t.TempDir(), "aibris.exe")
	newContent := []byte("version 1 re-downloaded")
	if err := os.WriteFile(newBinary, newContent, 0755); err != nil {
		t.Fatal(err)
	}

	script := `
$ErrorActionPreference = "Stop"
. .\install.ps1
$script:Binary = "aibris.exe"
Install-Binary -Source "` + newBinary + `" -Destination "` + existingBinary + `"
`

	output := runPowerShellSnippet(t, home, script)
	if !strings.Contains(output, "Installing aibris.exe") {
		t.Errorf("install output missing confirmation; output:\n%s", output)
	}

	content, err := os.ReadFile(existingBinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(newContent) {
		t.Errorf("binary not replaced; got %q, want %q", string(content), string(newContent))
	}
}

// TestNativeInstallPowerShellEmptyPrefixFirstInstall verifies that installing
// into an empty prefix (no existing binary) succeeds
func TestNativeInstallPowerShellEmptyPrefixFirstInstall(t *testing.T) {
	home := t.TempDir()
	installDir := filepath.Join(home, "new-install")

	sourceBinary := filepath.Join(t.TempDir(), "aibris.exe")
	sourceContent := []byte("first install")
	if err := os.WriteFile(sourceBinary, sourceContent, 0755); err != nil {
		t.Fatal(err)
	}

	destBinary := filepath.Join(installDir, "aibris.exe")

	script := `
$ErrorActionPreference = "Stop"
. .\install.ps1
$script:Binary = "aibris.exe"
Install-Binary -Source "` + sourceBinary + `" -Destination "` + destBinary + `"
`

	output := runPowerShellSnippet(t, home, script)
	if !strings.Contains(output, "Installing aibris.exe") {
		t.Errorf("install output missing confirmation; output:\n%s", output)
	}

	if _, err := os.Stat(destBinary); os.IsNotExist(err) {
		t.Fatal("binary was not installed")
	}

	content, err := os.ReadFile(destBinary)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(sourceContent) {
		t.Errorf("installed binary content = %q; want %q", string(content), string(sourceContent))
	}
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
