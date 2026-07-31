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
	"runtime"
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
	binaryName := "aibris"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	cliContractBinary = filepath.Join(buildDir, binaryName)
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
	values := make(map[string]string, len(extraEnv)+9)
	for key, value := range extraEnv {
		values[key] = value
	}
	homeDrive := filepath.VolumeName(home)
	values["HOME"] = home
	values["USERPROFILE"] = home
	values["HOMEDRIVE"] = homeDrive
	values["HOMEPATH"] = strings.TrimPrefix(home, homeDrive)
	values["XDG_CACHE_HOME"] = cache
	values["LOCALAPPDATA"] = cache
	values["TMPDIR"] = temp
	values["TMP"] = temp
	values["TEMP"] = temp
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
		"USERPROFILE":    true,
		"HOMEDRIVE":      true,
		"HOMEPATH":       true,
		"XDG_CACHE_HOME": true,
		"LOCALAPPDATA":   true,
		"TMPDIR":         true,
		"TMP":            true,
		"TEMP":           true,
	}
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !blocked[strings.ToUpper(key)] {
			env = append(env, entry)
		}
	}
	return env
}

func TestCLIContractDestructiveCommandIsolatesWindowsUserCache(t *testing.T) {
	externalCache := t.TempDir()
	t.Setenv("LoCaLaPpDaTa", externalCache)

	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	writeCLIContractFixture(t, filepath.Join(modules, "package", "sentinel"), "remove")
	makeCLIContractTargetOld(t, modules)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := newCLIContractCommand(t, ctx, home, nil,
		"clean", "--force", "--no-guide", "--age=1h", "--category=node_modules")
	localAppDataCount := 0
	for _, entry := range command.Env {
		key, value, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "LOCALAPPDATA") {
			continue
		}
		localAppDataCount++
		if want := filepath.Join(home, ".cache"); value != want {
			t.Fatalf("LOCALAPPDATA = %q, want isolated cache %q", value, want)
		}
	}
	if localAppDataCount != 1 {
		t.Fatalf("case-insensitive LOCALAPPDATA entries = %d, want 1: %q",
			localAppDataCount, command.Env)
	}

	result := runCLIContract(t, home, nil,
		"clean", "--force", "--no-guide", "--age=1h", "--category=node_modules")
	if result.ExitCode != 0 {
		t.Fatalf("destructive cleanup exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	if _, err := os.Lstat(modules); !os.IsNotExist(err) {
		t.Fatalf("destructive fixture target removal error = %v; want not exist", err)
	}
	if entries, err := os.ReadDir(externalCache); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("destructive fixture touched external Windows user cache: %v", entries)
	}
	isolatedScanCaches := []string{
		filepath.Join(home, ".cache", "aibris", "last-scan.json"),
		filepath.Join(home, "Library", "Caches", "aibris", "last-scan.json"),
	}
	for _, path := range isolatedScanCaches {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("destructive cleanup left reusable scan cache %q: %v", path, err)
		}
	}
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

func TestCLIContractCleanHelpMatchesPolicyVocabulary(t *testing.T) {
	result := runCLIContract(t, t.TempDir(), nil, "clean", "--help")
	if result.ExitCode != 0 {
		t.Fatalf("clean --help exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	for _, want := range []string{
		"Minimum idle age",
		"unknown",
		"selected targets enter the cleanup plan",
		"reviewable targets",
		"protected targets",
		"review displays protected targets as locked rows",
	} {
		if !strings.Contains(result.Stdout, want) {
			t.Errorf("clean --help missing %q:\n%s", want, result.Stdout)
		}
	}
	if strings.Contains(result.Stdout, "Max age") {
		t.Errorf("clean --help still describes --age as a maximum:\n%s", result.Stdout)
	}
}

func TestCLIContractShortClassicAgeWarningPreservesScopeAndProtections(t *testing.T) {
	home := t.TempDir()
	result := runCLIContract(
		t,
		home,
		nil,
		"clean",
		"--no-guide",
		"--dry-run",
		"--root", home,
		"--category=node_modules",
		"--age=30m",
	)
	if result.ExitCode != 0 {
		t.Fatalf("short-age dry-run exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	for _, want := range []string{
		"very low classic minimum-age threshold",
		"selected category/tool scope",
		"safety protections still apply",
	} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("short-age warning missing %q:\n%s", want, result.Stderr)
		}
	}
	if strings.Contains(result.Stderr, "match ALL items including active ones") {
		t.Errorf("short-age warning still overstates active matching:\n%s", result.Stderr)
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

func TestCLIContractNestedAgentStateDestructiveSafety(t *testing.T) {
	t.Run("claude narrow live entry refuses without mutation", func(t *testing.T) {
		home := t.TempDir()
		entry := filepath.Join(home, ".claude", "projects", "live-parent")
		modules := filepath.Join(entry, "node_modules")
		moduleSentinel := filepath.Join(modules, "package", "sentinel")
		entrySentinel := filepath.Join(entry, "kept", "sentinel")
		liveCWD := filepath.Join(home, "workspace", "live")
		liveSentinel := filepath.Join(liveCWD, "sentinel")
		writeCLIContractFixture(t, moduleSentinel, "must survive")
		writeCLIContractFixture(t, entrySentinel, "must survive")
		writeCLIContractFixture(t, liveSentinel, "must survive")
		session := filepath.Join(entry, "session.jsonl")
		writeCLIContractFixture(t, session, fmt.Sprintf("{\"cwd\":%q}\n", liveCWD))
		makeCLIContractTargetOld(t, modules)

		result := runCLIContract(t, home, nil,
			"clean", "--force", "--no-guide", "--age=1h",
			"--category=node_modules", "--root="+entry)
		if result.ExitCode != 0 {
			t.Fatalf("protected cleanup exit = %d\nstdout:\n%s\nstderr:\n%s",
				result.ExitCode, result.Stdout, result.Stderr)
		}
		for _, want := range []string{
			"safety  refused protected agent-state ancestor",
			"No items to clean.",
		} {
			if !strings.Contains(result.Stdout, want) {
				t.Errorf("protected cleanup stdout missing %q: %s", want, result.Stdout)
			}
		}
		if strings.Contains(result.Stdout, "cleanup receipt") {
			t.Errorf("protected cleanup crossed the execution boundary: %s", result.Stdout)
		}
		if result.Stderr != "" {
			t.Errorf("protected cleanup stderr = %q", result.Stderr)
		}
		for _, path := range []string{moduleSentinel, entrySentinel, session, liveSentinel} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("protected cleanup changed %q: %v", path, err)
			}
		}
	})

	t.Run("cursor narrow live entry refuses without mutation", func(t *testing.T) {
		home := t.TempDir()
		entry := filepath.Join(home, ".cursor", "projects", "live-parent")
		modules := filepath.Join(entry, "node_modules")
		moduleSentinel := filepath.Join(modules, "package", "sentinel")
		entrySentinel := filepath.Join(entry, "kept", "sentinel")
		liveCWD := filepath.Join(home, "workspace", "live")
		liveSentinel := filepath.Join(liveCWD, "sentinel")
		writeCLIContractFixture(t, moduleSentinel, "must survive")
		writeCLIContractFixture(t, entrySentinel, "must survive")
		writeCLIContractFixture(t, liveSentinel, "must survive")
		workerLog := filepath.Join(entry, "worker.log")
		writeCLIContractFixture(t, workerLog, "[info] workspacePath="+liveCWD+"\n")
		makeCLIContractTargetOld(t, modules)

		result := runCLIContract(t, home, nil,
			"clean", "--force", "--no-guide", "--age=1h",
			"--category=node_modules", "--root="+entry)
		if result.ExitCode != 0 {
			t.Fatalf("protected cleanup exit = %d\nstdout:\n%s\nstderr:\n%s",
				result.ExitCode, result.Stdout, result.Stderr)
		}
		for _, want := range []string{
			"safety  refused protected agent-state ancestor",
			"No items to clean.",
		} {
			if !strings.Contains(result.Stdout, want) {
				t.Errorf("protected cleanup stdout missing %q: %s", want, result.Stdout)
			}
		}
		if strings.Contains(result.Stdout, "cleanup receipt") {
			t.Errorf("protected cleanup crossed the execution boundary: %s", result.Stdout)
		}
		if result.Stderr != "" {
			t.Errorf("protected cleanup stderr = %q", result.Stderr)
		}
		for _, path := range []string{moduleSentinel, entrySentinel, workerLog, liveSentinel} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("protected cleanup changed %q: %v", path, err)
			}
		}
	})

	t.Run("orphan parent revalidates and removes exactly one target", func(t *testing.T) {
		home := t.TempDir()
		entry := filepath.Join(home, ".claude", "projects", "orphan-parent")
		modules := filepath.Join(entry, "node_modules")
		moduleSentinel := filepath.Join(modules, "package", "sentinel")
		keptSentinel := filepath.Join(entry, "kept", "sentinel")
		missingCWD := filepath.Join(home, "missing", "project")
		writeCLIContractFixture(t, moduleSentinel, "remove once")
		writeCLIContractFixture(t, keptSentinel, "must remain")
		session := filepath.Join(entry, "session.jsonl")
		writeCLIContractFixture(t, session, fmt.Sprintf("{\"cwd\":%q}\n", missingCWD))
		makeCLIContractTargetOld(t, modules)

		result := runCLIContract(t, home, nil,
			"clean", "--force", "--no-guide", "--age=1h",
			"--category=node_modules", "--root="+entry)
		if result.ExitCode != 0 {
			t.Fatalf("orphan cleanup exit = %d\nstdout:\n%s\nstderr:\n%s",
				result.ExitCode, result.Stdout, result.Stderr)
		}
		for _, want := range []string{
			"cleanup receipt",
			"targets    1 item",
			"removed    1 item",
			"failed     0 items",
		} {
			if !strings.Contains(result.Stdout, want) {
				t.Errorf("orphan cleanup stdout missing %q: %s", want, result.Stdout)
			}
		}
		if result.Stderr != "" {
			t.Errorf("orphan cleanup stderr = %q", result.Stderr)
		}
		if _, err := os.Lstat(modules); !os.IsNotExist(err) {
			t.Fatalf("orphan nested target removal error = %v; want not exist", err)
		}
		for _, path := range []string{entry, session, keptSentinel} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("orphan cleanup changed non-target %q: %v", path, err)
			}
		}
	})
}

func writeCLIContractFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func makeCLIContractTargetOld(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
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
