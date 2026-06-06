package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runInstallSnippet(t *testing.T, home, script string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-c", script, "bash"}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = "."
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin",
		"SHELL=/bin/zsh",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestInstallScriptDefaultDirIsUserLocal(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
printf 'dir=%s\nexplicit=%s\n' "$INSTALL_DIR" "$INSTALL_DIR_EXPLICIT"
`)

	if !strings.Contains(output, "dir="+filepath.Join(home, ".local", "bin")) {
		t.Fatalf("default install dir not user-local; output:\n%s", output)
	}
	if !strings.Contains(output, "explicit=0") {
		t.Fatalf("default install dir should not be explicit; output:\n%s", output)
	}
}

func TestInstallScriptPrefixIsExplicitAndExpandsHome(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
parse_args --prefix '~/bin' 0.3.3
INSTALL_DIR="$(expand_path "$INSTALL_DIR")"
printf 'dir=%s\nexplicit=%s\nversion=%s\n' "$INSTALL_DIR" "$INSTALL_DIR_EXPLICIT" "$VERSION"
`)

	if !strings.Contains(output, "dir="+filepath.Join(home, "bin")) {
		t.Fatalf("prefix was not expanded under HOME; output:\n%s", output)
	}
	if !strings.Contains(output, "explicit=1") {
		t.Fatalf("prefix should mark install dir explicit; output:\n%s", output)
	}
	if !strings.Contains(output, "version=0.3.3") {
		t.Fatalf("version argument not parsed; output:\n%s", output)
	}
}

func TestInstallScriptPathHintUsesHomeVariable(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
INSTALL_DIR="$HOME/.local/bin"
print_path_hint
`)

	for _, want := range []string{
		"aibris was installed to ~/.local/bin",
		`echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc`,
		`export PATH="$HOME/.local/bin:$PATH"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("path hint missing %q; output:\n%s", want, output)
		}
	}
}

func TestInstallScriptPathHintSkipsWhenAlreadyOnPath(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
INSTALL_DIR="$HOME/.local/bin"
PATH="$INSTALL_DIR:/usr/bin:/bin"
print_path_hint
`)

	if output != "" {
		t.Fatalf("expected no PATH hint when install dir is already on PATH; output:\n%s", output)
	}
}

func TestInstallScriptInstallBinaryToDefaultDirWithoutSudo(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "aibris")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nprintf 'aibris test\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	output := runInstallSnippet(t, home, `
source ./install.sh
INSTALL_DIR="$(expand_path "$INSTALL_DIR")"
install_binary "$1"
"$INSTALL_DIR/aibris"
`, source)

	if strings.Contains(output, "Using sudo") {
		t.Fatalf("default user-local install should not use sudo; output:\n%s", output)
	}
	if !strings.Contains(output, "Installed aibris to "+filepath.Join(home, ".local", "bin", "aibris")) {
		t.Fatalf("install output missing destination; output:\n%s", output)
	}
	if !strings.Contains(output, "aibris test") {
		t.Fatalf("installed binary did not run; output:\n%s", output)
	}
}
