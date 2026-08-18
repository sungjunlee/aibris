package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runPourSnippet(t *testing.T, script string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"-c", script, "bash"}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustRunPourSnippet(t *testing.T, script string, args ...string) string {
	t.Helper()
	out, err := runPourSnippet(t, script, args...)
	if err != nil {
		t.Fatalf("pour snippet failed: %v\n%s", err, out)
	}
	return out
}

func TestPourExpectedVersionLine(t *testing.T) {
	out := mustRunPourSnippet(t, `
source .github/scripts/pour-homebrew-formula.sh
pour_expected_version_line "$1"
`, "v0.11.0")
	if out != "aibris version 0.11.0\n" {
		t.Fatalf("expected version line = %q, got %q", "aibris version 0.11.0\n", out)
	}
}

func TestPourAssertVersion(t *testing.T) {
	tests := []struct {
		name    string
		got     string
		want    string
		wantErr bool
	}{
		{name: "match", got: "aibris version 0.11.0", want: "aibris version 0.11.0"},
		{name: "previous patch", got: "aibris version 0.10.0", want: "aibris version 0.11.0", wantErr: true},
		{name: "dev", got: "aibris version dev", want: "aibris version 0.11.0", wantErr: true},
		{name: "snapshot", got: "aibris version 0.11.0-next", want: "aibris version 0.11.0", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runPourSnippet(t, `
source .github/scripts/pour-homebrew-formula.sh
pour_assert_version "$1" "$2"
`, tt.got, tt.want)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v; output: %s", err, tt.wantErr, out)
			}
		})
	}
}

func TestPourShouldUpgrade(t *testing.T) {
	out := mustRunPourSnippet(t, `
source .github/scripts/pour-homebrew-formula.sh
if pour_should_upgrade 0; then echo zero:0; else echo zero:1; fi
if pour_should_upgrade 1; then echo one:0; else echo one:1; fi
if pour_should_upgrade 2; then echo two:0; else echo two:1; fi
`)
	if !strings.Contains(out, "zero:1") || !strings.Contains(out, "one:1") || !strings.Contains(out, "two:0") {
		t.Fatalf("upgrade threshold = %q", out)
	}
}

func TestPourFormulaRevisions(t *testing.T) {
	empty := t.TempDir()
	one := initTapRepo(t, "url v0.10.0")
	two := initTapRepo(t, "url v0.10.0", "url v0.11.0")

	out := mustRunPourSnippet(t, `
source .github/scripts/pour-homebrew-formula.sh
pour_formula_revisions "$1"
pour_formula_revisions "$2"
pour_formula_revisions "$3"
`, empty, one, two)
	if out != "0\n1\n2\n" {
		t.Fatalf("revision counts = %q", out)
	}
}

func TestPourScriptStaysInsideTheTap(t *testing.T) {
	script := readRepoFile(t, filepath.Join(".github", "scripts", "pour-homebrew-formula.sh"))
	for _, forbidden := range []string{
		"--depth=",
		"brew install --formula",
		"brew upgrade --formula",
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("pour script must not use %q", forbidden)
		}
	}
	for _, required := range []string{
		`brew install "${TAP_NAME}/${FORMULA_NAME}"`,
		`brew upgrade "${TAP_NAME}/${FORMULA_NAME}"`,
		`brew --repo "${TAP_NAME}"`,
		`revisions="$(pour_formula_revisions "$tap_dir")"`,
	} {
		if !strings.Contains(script, required) {
			t.Errorf("pour script is missing %q", required)
		}
	}
}

func TestPourResolveTag(t *testing.T) {
	out := mustRunPourSnippet(t, `
source .github/scripts/pour-homebrew-formula.sh
pour_resolve_tag "$1"
`, "v0.11.0")
	if out != "v0.11.0\n" {
		t.Fatalf("resolved tag = %q", out)
	}

	_, err := runPourSnippet(t, `
unset GITHUB_REF_NAME
source .github/scripts/pour-homebrew-formula.sh
pour_resolve_tag
`)
	if err == nil {
		t.Fatal("expected missing tag to fail")
	}
}

func initTapRepo(t *testing.T, formulas ...string) string {
	t.Helper()
	dir := t.TempDir()
	formulaDir := filepath.Join(dir, "Formula")
	if err := os.MkdirAll(formulaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "test")
	for i, body := range formulas {
		path := filepath.Join(formulaDir, "aibris.rb")
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "Formula/aibris.rb")
		runGit(t, dir, "commit", "-m", "formula "+string(rune('1'+i)))
	}
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
