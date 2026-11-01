package test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFmtCheckPreservesGofmtStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Makefile is a Unix developer entry point; native Windows uses explicit package commands")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	t.Run("missing input", func(t *testing.T) {
		missing := filepath.Join(dir, "missing.go")
		out, err := runFmtCheck(t, root, missing)
		if err == nil {
			t.Fatalf("fmt-check succeeded for missing input:\n%s", out)
		}
		if !strings.Contains(string(out), "no such file") {
			t.Fatalf("fmt-check diagnostic = %q; want the gofmt read error", out)
		}
	})

	t.Run("formatted file", func(t *testing.T) {
		path := filepath.Join(dir, "good.go")
		body := []byte("package p\n\nfunc f() {}\n")
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
		out, err := runFmtCheck(t, root, path)
		if err != nil {
			t.Fatalf("fmt-check error = %v\n%s", err, out)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("formatted file changed to %q: %v", got, err)
		}
	})

	t.Run("unformatted file", func(t *testing.T) {
		path := filepath.Join(dir, "bad.go")
		body := []byte("package p\nfunc f(){}\n")
		if err := os.WriteFile(path, body, 0644); err != nil {
			t.Fatal(err)
		}
		out, err := runFmtCheck(t, root, path)
		if err == nil || !strings.Contains(string(out), path) || !strings.Contains(string(out), "gofmt needed:") {
			t.Fatalf("fmt-check error = %v\n%s", err, out)
		}
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("fmt-check rewrote %q: %v", got, err)
		}
	})
}

func runFmtCheck(t *testing.T, root, goFile string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("make", "fmt-check", "GO_FILES="+goFile)
	cmd.Dir = root
	return cmd.CombinedOutput()
}
