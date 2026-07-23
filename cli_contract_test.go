package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var cliContractBinary string

func TestMain(m *testing.M) {
	buildDir, err := os.MkdirTemp("", "aibris-cli-contract-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create CLI contract build directory: %v\n", err)
		os.Exit(1)
	}
	cliContractBinary = filepath.Join(buildDir, "aibris")
	build := exec.Command("go", "build", "-o", cliContractBinary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build CLI contract binary: %v\n%s", err, output)
		_ = os.RemoveAll(buildDir)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.RemoveAll(buildDir)
	os.Exit(code)
}

type cliContractResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func runCLIContract(t *testing.T, home string, extraEnv map[string]string, args ...string) cliContractResult {
	return runCLIContractWithInput(t, home, extraEnv, "", args...)
}

func runCLIContractWithInput(t *testing.T, home string, extraEnv map[string]string, input string, args ...string) cliContractResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := newCLIContractCommand(t, ctx, home, extraEnv, args...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("aibris %v timed out\nstdout:\n%s\nstderr:\n%s", args, stdout.String(), stderr.String())
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("aibris %v failed to start: %v", args, err)
		}
		exitCode = exitErr.ExitCode()
	}
	return cliContractResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

func newCLIContractCommand(t *testing.T, ctx context.Context, home string, extraEnv map[string]string, args ...string) *exec.Cmd {
	t.Helper()
	cache := filepath.Join(home, ".cache")
	temp := filepath.Join(home, "tmp")
	for _, dir := range []string{home, cache, temp} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}

	env := filteredCLIContractEnv()
	values := map[string]string{
		"HOME":           home,
		"XDG_CACHE_HOME": cache,
		"TMPDIR":         temp,
	}
	for key, value := range extraEnv {
		values[key] = value
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}

	cmd := exec.CommandContext(ctx, cliContractBinary, args...)
	cmd.Dir = home
	cmd.Env = env
	return cmd
}

func filteredCLIContractEnv() []string {
	blocked := map[string]bool{
		"HOME":           true,
		"XDG_CACHE_HOME": true,
		"TMPDIR":         true,
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !blocked[key] {
			env = append(env, entry)
		}
	}
	return env
}

func TestCLIContractInvalidFlag(t *testing.T) {
	result := runCLIContract(t, t.TempDir(), nil, "scan", "--not-a-real-flag")
	if result.ExitCode == 0 {
		t.Fatalf("invalid flag exited 0\nstdout:\n%s\nstderr:\n%s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "unknown flag") {
		t.Errorf("stderr missing unknown flag error: %s", result.Stderr)
	}
}

func TestCLIContractInvalidSelectors(t *testing.T) {
	for _, flag := range []string{"category", "tool"} {
		t.Run(flag, func(t *testing.T) {
			result := runCLIContract(t, t.TempDir(), nil, "clean", "--dry-run", "--"+flag, "mystery")
			if result.ExitCode == 0 {
				t.Fatalf("invalid %s exited 0\nstdout:\n%s\nstderr:\n%s", flag, result.Stdout, result.Stderr)
			}
			if !strings.Contains(result.Stderr, `invalid --`+flag+` value "mystery"`) {
				t.Errorf("stderr missing selector error: %s", result.Stderr)
			}
			if strings.Contains(result.Stdout, "scanning") {
				t.Errorf("invalid selector scanned before failing: %s", result.Stdout)
			}
		})
	}
}

func TestCLIContractInvalidRoot(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	result := runCLIContract(t, home, nil, "scan", "--root", outside)
	if result.ExitCode == 0 {
		t.Fatalf("invalid root exited 0\nstdout:\n%s\nstderr:\n%s", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stderr, "must be under") {
		t.Errorf("stderr missing root boundary error: %s", result.Stderr)
	}
}

func TestCLIContractDryRunDoesNotDelete(t *testing.T) {
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil,
		"clean", "--dry-run", "--no-guide", "--age=1h", "--category=node_modules")
	if result.ExitCode != 0 {
		t.Fatalf("dry-run exit = %d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	for _, want := range []string{"scan summary", "clean plan", "[DRY-RUN] No files were removed."} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("dry-run stdout missing %q: %s", want, result.Stdout)
		}
	}
	if result.Stderr != "" {
		t.Errorf("dry-run stderr = %q", result.Stderr)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("dry-run removed target: %v", err)
	}
}

func TestCLIContractDeclinedPromptDoesNotDelete(t *testing.T) {
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}

	result := runCLIContractWithInput(t, home, nil, "n\n",
		"clean", "--no-guide", "--age=1h", "--category=node_modules")
	if result.ExitCode != 0 {
		t.Fatalf("declined prompt exit = %d\nstdout:\n%s\nstderr:\n%s", result.ExitCode, result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Proceed? [y/N]: Aborted.") {
		t.Errorf("declined prompt stdout missing abort contract: %s", result.Stdout)
	}
	if result.Stderr != "" {
		t.Errorf("declined prompt stderr = %q", result.Stderr)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("declined prompt removed target: %v", err)
	}
}

func TestCLIContractCancellation(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 3000; i++ {
		if err := os.MkdirAll(filepath.Join(home, "workspace", fmt.Sprintf("project-%04d", i)), 0755); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := newCLIContractCommand(t, ctx, home, nil, "scan")
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	reader := bufio.NewReader(stdoutPipe)
	var stdout strings.Builder
	line, err := reader.ReadString('\n')
	stdout.WriteString(line)
	if err != nil || strings.TrimSpace(line) != "scan" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("scan header handshake failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("send interrupt: %v", err)
	}
	remaining, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	stdout.Write(remaining)
	err = cmd.Wait()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("cancelled scan timed out\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("cancelled scan did not exit non-zero: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "context canceled") {
		t.Errorf("cancellation stderr missing context error: %s", stderr.String())
	}
}

func TestCLIContractCleanupFailure(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, ".cache", "go-build")
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "entry"), []byte("cache"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(cache, old, old); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(home, "bin")
	if err := os.MkdirAll(fakeBin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "go"), []byte("#!/bin/sh\nexit 23\n"), 0755); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, map[string]string{"PATH": fakeBin},
		"clean", "--force", "--no-guide", "--age=1h", "--category=build-cache")
	if result.ExitCode == 0 {
		t.Fatalf("cleanup failure exited 0\nstdout:\n%s\nstderr:\n%s", result.Stdout, result.Stderr)
	}
	for _, want := range []string{"cleanup receipt", "failed     1 item", "freed      0 B"} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("cleanup failure stdout missing %q: %s", want, result.Stdout)
		}
	}
	if !strings.Contains(result.Stderr, "error during cleanup") {
		t.Errorf("cleanup failure stderr missing execution error: %s", result.Stderr)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Fatalf("failed command unexpectedly removed cache: %v", err)
	}
}
