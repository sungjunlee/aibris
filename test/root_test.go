package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var cliContractBinary string

func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		mod := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(mod)
		if err == nil && isAibrisGoMod(data) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("module root not found from %s", file)
		}
		dir = parent
	}
}

func isAibrisGoMod(data []byte) bool {
	text := strings.TrimPrefix(string(data), "\ufeff")
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line) == "module github.com/sungjunlee/aibris"
}

func TestMain(m *testing.M) {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "locate module root: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "chdir to module root: %v\n", err)
		os.Exit(1)
	}

	buildDir, err := os.MkdirTemp("", "aibris-cli-contract-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI contract build directory: %v\n", err)
		os.Exit(1)
	}
	binaryName := "aibris"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	cliContractBinary = filepath.Join(buildDir, binaryName)
	build := exec.Command("go", "build", "-o", cliContractBinary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI contract binary: %v\n%s", err, output)
		_ = os.RemoveAll(buildDir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(buildDir)
	os.Exit(code)
}

func TestIsAibrisGoModAcceptsWindowsLineEndings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "lf", data: "module github.com/sungjunlee/aibris\n\ngo 1.26.3\n", want: true},
		{name: "crlf", data: "module github.com/sungjunlee/aibris\r\n\r\ngo 1.26.3\r\n", want: true},
		{name: "bom crlf", data: "\ufeffmodule github.com/sungjunlee/aibris\r\n", want: true},
		{name: "other module", data: "module example.com/other\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isAibrisGoMod([]byte(tt.data)); got != tt.want {
				t.Fatalf("isAibrisGoMod(%q) = %t; want %t", tt.data, got, tt.want)
			}
		})
	}
}
