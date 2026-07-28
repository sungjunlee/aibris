package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

func TestAgentStateCLIContract(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()

	claudeOrphan := filepath.Join(home, ".claude", "projects", "claude-orphan")
	claudeOrphanCWD := filepath.Join(home, "missing", "claude-project")
	claudeOrphanEvidence := fmt.Sprintf(
		"{\"cwd\":%q}\n",
		claudeOrphanCWD,
	)
	writeCLIContractFile(t, filepath.Join(claudeOrphan, "session.jsonl"),
		claudeOrphanEvidence)
	claudeLiveCWD := filepath.Join(home, "workspace", "claude-live")
	if err := os.MkdirAll(claudeLiveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	claudeLive := filepath.Join(home, ".claude", "projects", "claude-live")
	writeCLIContractFile(t, filepath.Join(claudeLive, "session.jsonl"),
		fmt.Sprintf("{\"cwd\":%q}\n", claudeLiveCWD))

	cursorOrphan := filepath.Join(home, ".cursor", "projects", "cursor-orphan")
	cursorOrphanCWD := filepath.Join(home, "missing", "cursor-project")
	writeCLIContractFile(t, filepath.Join(cursorOrphan, "worker.log"),
		"[info] workspacePath="+cursorOrphanCWD+"\n")
	cursorUndetermined := filepath.Join(home, ".cursor", "projects", "cursor-undetermined")
	if err := os.MkdirAll(cursorUndetermined, 0755); err != nil {
		t.Fatal(err)
	}

	assertCLIContractHomeIsolated(t, binary, home,
		claudeOrphan, claudeLive, cursorOrphan, cursorUndetermined)

	output, err := runCLIContract(binary, home, "clean", "--dry-run", "--category", "agent-state")
	if err != nil {
		t.Fatalf("agent-state dry-run failed: %v\n%s", err, output)
	}
	auditFields := strings.Fields(cliContractLineWithPrefix(t, output, "agent-state"))
	if len(auditFields) < 10 {
		t.Fatalf("agent-state audit row has %d fields, want at least 10: %q", len(auditFields), auditFields)
	}
	if auditFields[1] != "4" || auditFields[4] != "2" || auditFields[7] != "2" {
		t.Fatalf("agent-state audit counts = found %s / eligible %s / protected %s, want 4/2/2\n%s",
			auditFields[1], auditFields[4], auditFields[7], output)
	}
	for _, want := range []string{
		"matched  2 candidates",
		"targets  2 items",
		"[DRY-RUN] No files were removed.",
		filepath.Base(claudeOrphan),
		claudeOrphanCWD,
		filepath.Base(cursorOrphan),
		cursorOrphanCWD,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("agent-state dry-run missing %q:\n%s", want, output)
		}
	}
	for _, protected := range []string{filepath.Base(claudeLive), filepath.Base(cursorUndetermined)} {
		if strings.Contains(output, protected) {
			t.Fatalf("protected path %q leaked into dry-run targets:\n%s", protected, output)
		}
	}
	if count := strings.Count(output, "remove-path"); count != 2 {
		t.Fatalf("dry-run target rows = %d, want 2:\n%s", count, output)
	}

	for _, toolCase := range []struct {
		tool     string
		selected string
		excluded string
	}{
		{tool: "claude", selected: filepath.Base(claudeOrphan), excluded: filepath.Base(cursorOrphan)},
		{tool: "cursor", selected: filepath.Base(cursorOrphan), excluded: filepath.Base(claudeOrphan)},
	} {
		t.Run(toolCase.tool, func(t *testing.T) {
			toolOutput, toolErr := runCLIContract(
				binary,
				home,
				"clean",
				"--dry-run",
				"--category",
				"agent-state",
				"--tool",
				toolCase.tool,
			)
			if toolErr != nil {
				t.Fatalf("%s selector failed: %v\n%s", toolCase.tool, toolErr, toolOutput)
			}
			for _, want := range []string{"matched  1 candidate", "targets  1 item", toolCase.selected} {
				if !strings.Contains(toolOutput, want) {
					t.Fatalf("%s selector missing %q:\n%s", toolCase.tool, want, toolOutput)
				}
			}
			if strings.Contains(toolOutput, toolCase.excluded) {
				t.Fatalf("%s selector included %q:\n%s", toolCase.tool, toolCase.excluded, toolOutput)
			}
		})
	}

	invalidOutput, invalidErr := runCLIContract(
		binary,
		home,
		"clean",
		"--dry-run",
		"--category",
		"not-a-category",
	)
	if invalidErr == nil {
		t.Fatalf("invalid category succeeded:\n%s", invalidOutput)
	}
	for _, want := range []string{`invalid --category value "not-a-category"`, "valid values:", "agent-state"} {
		if !strings.Contains(invalidOutput, want) {
			t.Fatalf("invalid category output missing %q:\n%s", want, invalidOutput)
		}
	}

	receiptOutput, receiptErr := runCLIContract(
		binary,
		home,
		"clean",
		"--force",
		"--category",
		"agent-state",
		"--tool",
		"claude",
	)
	if receiptErr != nil {
		t.Fatalf("agent-state cleanup failed: %v\n%s", receiptErr, receiptOutput)
	}
	for _, want := range []string{
		"cleanup receipt",
		"targets    1 item",
		"removed    1 item",
		"failed     0 items",
		"freed      " + cleaner.FormatSize(int64(len(claudeOrphanEvidence))),
	} {
		if !strings.Contains(receiptOutput, want) {
			t.Fatalf("agent-state receipt missing %q:\n%s", want, receiptOutput)
		}
	}
	if _, statErr := os.Stat(claudeOrphan); !os.IsNotExist(statErr) {
		t.Fatalf("orphaned Claude state still exists after cleanup: %v", statErr)
	}
	if _, statErr := os.Stat(claudeLive); statErr != nil {
		t.Fatalf("live Claude state changed during cleanup: %v", statErr)
	}
}

func buildCLIContractBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "aibris")
	command := exec.Command("go", "build", "-o", binary, ".")
	command.Dir = ".."
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("building CLI contract binary: %v\n%s", err, output)
	}
	return binary
}

func runCLIContract(binary, home string, args ...string) (string, error) {
	command := exec.Command(binary, args...)
	command.Env = cliContractEnv(os.Environ(), home)
	output, err := command.CombinedOutput()
	return string(output), err
}

func cliContractEnv(environ []string, home string) []string {
	isolatedKeys := map[string]bool{
		"HOME":           true,
		"USERPROFILE":    true,
		"HOMEDRIVE":      true,
		"HOMEPATH":       true,
		"XDG_CACHE_HOME": true,
		"LOCALAPPDATA":   true,
	}
	env := make([]string, 0, len(environ)+len(isolatedKeys))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if isolatedKeys[strings.ToUpper(key)] {
			continue
		}
		env = append(env, entry)
	}

	homeDrive := filepath.VolumeName(home)
	homePath := strings.TrimPrefix(home, homeDrive)
	cacheHome := filepath.Join(home, ".cache")
	return append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"HOMEDRIVE="+homeDrive,
		"HOMEPATH="+homePath,
		"XDG_CACHE_HOME="+cacheHome,
		"LOCALAPPDATA="+cacheHome,
	)
}

func assertCLIContractHomeIsolated(t *testing.T, binary, home string, fixturePaths ...string) {
	t.Helper()

	output, err := runCLIContract(binary, home, "scan", "--json")
	if err != nil {
		t.Fatalf("home isolation scan failed: %v\n%s", err, output)
	}
	var scan jsonOutput
	if err := json.Unmarshal([]byte(output), &scan); err != nil {
		t.Fatalf("decoding home isolation scan: %v\n%s", err, output)
	}

	found := make(map[string]bool, len(scan.Worktrees))
	for _, item := range scan.Worktrees {
		path := filepath.Clean(item.Path)
		relative, err := filepath.Rel(home, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("home isolation failed: scan reported non-fixture path %q for fixture home %q",
				item.Path, home)
		}
		found[path] = true
	}
	for _, path := range fixturePaths {
		if !found[filepath.Clean(path)] {
			t.Fatalf("home isolation failed: scan did not report fixture path %q\n%s", path, output)
		}
	}
}

func TestCLIContractEnvReplacesHomeVariables(t *testing.T) {
	home := t.TempDir()
	environ := []string{
		"PATH=/fixture/bin",
		"HOME=/old/home",
		"home=/duplicate/home",
		"USERPROFILE=C:\\old\\profile",
		"HOMEDRIVE=C:",
		"HOMEPATH=\\old\\path",
		"XDG_CACHE_HOME=/old/cache",
		"LOCALAPPDATA=C:\\old\\cache",
	}

	env := cliContractEnv(environ, home)
	want := map[string]string{
		"HOME":           home,
		"USERPROFILE":    home,
		"HOMEDRIVE":      filepath.VolumeName(home),
		"HOMEPATH":       strings.TrimPrefix(home, filepath.VolumeName(home)),
		"XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"LOCALAPPDATA":   filepath.Join(home, ".cache"),
	}
	counts := make(map[string]int, len(want))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		expected, ok := want[key]
		if !ok {
			continue
		}
		counts[key]++
		if value != expected {
			t.Errorf("%s = %q, want %q", key, value, expected)
		}
	}
	for key := range want {
		if counts[key] != 1 {
			t.Errorf("%s entries = %d, want 1: %q", key, counts[key], env)
		}
	}
}

func writeCLIContractFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
}

func cliContractLineWithPrefix(t *testing.T, output, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix+" ") {
			return line
		}
	}
	t.Fatalf("output missing line with prefix %q:\n%s", prefix, output)
	return ""
}
