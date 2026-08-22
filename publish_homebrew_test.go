package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runPublishSnippet(t *testing.T, script string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"-c", script, "bash"}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "PATH=/usr/bin:/bin")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustRunPublishSnippet(t *testing.T, script string, args ...string) string {
	t.Helper()
	out, err := runPublishSnippet(t, script, args...)
	if err != nil {
		t.Fatalf("publish snippet failed: %v\n%s", err, out)
	}
	return out
}

func TestPublishFindFormulaExactlyOne(t *testing.T) {
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "aibris.rb"), []byte("class Aibris < Formula\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := mustRunPublishSnippet(t, `
source .github/scripts/publish-homebrew-formula.sh
publish_find_formula "$1"
`, dist)
	if !strings.HasSuffix(strings.TrimSpace(out), "aibris.rb") {
		t.Fatalf("formula path = %q", out)
	}
}

func TestPublishFindFormulaMissing(t *testing.T) {
	_, err := runPublishSnippet(t, `
source .github/scripts/publish-homebrew-formula.sh
publish_find_formula "$1"
`, t.TempDir())
	if err == nil {
		t.Fatal("expected missing formula to fail")
	}
}

func TestPublishFindFormulaMultiple(t *testing.T) {
	dist := t.TempDir()
	nested := filepath.Join(dist, "extra")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(dist, "aibris.rb"),
		filepath.Join(nested, "aibris.rb"),
	} {
		if err := os.WriteFile(path, []byte("class Aibris < Formula\nend\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := runPublishSnippet(t, `
source .github/scripts/publish-homebrew-formula.sh
publish_find_formula "$1"
`, dist)
	if err == nil {
		t.Fatal("expected multiple formulas to fail")
	}
}

func TestPublishRequireToken(t *testing.T) {
	_, err := runPublishSnippet(t, `
unset HOMEBREW_TAP_TOKEN
source .github/scripts/publish-homebrew-formula.sh
publish_require_token
`)
	if err == nil {
		t.Fatal("expected missing token to fail")
	}
}

func TestPublishCommitFormulaCreatesAndSkips(t *testing.T) {
	tap := initTapRepo(t, "url v0.10.0")
	formula := filepath.Join(t.TempDir(), "aibris.rb")
	if err := os.WriteFile(formula, []byte("url v0.11.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	first := mustRunPublishSnippet(t, `
source .github/scripts/publish-homebrew-formula.sh
publish_commit_formula "$1" "$2"
`, tap, formula)
	if !strings.Contains(first, "Brew formula update") && !fileContains(t, filepath.Join(tap, "Formula/aibris.rb"), "url v0.11.0") {
		t.Fatalf("first commit output = %q", first)
	}

	second := mustRunPublishSnippet(t, `
source .github/scripts/publish-homebrew-formula.sh
publish_commit_formula "$1" "$2"
`, tap, formula)
	if !strings.Contains(second, "already up to date") {
		t.Fatalf("second commit output = %q", second)
	}
}

func fileContains(t *testing.T, path, want string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(data), want)
}
