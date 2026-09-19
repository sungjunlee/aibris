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
		if err == nil && strings.HasPrefix(string(data), "module github.com/sungjunlee/aibris\n") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("module root not found from %s", file)
		}
		dir = parent
	}
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
