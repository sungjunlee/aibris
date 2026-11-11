package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillJSONWorkflowContract(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "skills", "aibris", "SKILL.md"))
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	text := string(raw)
	for _, want := range []string{
		" --json --include-paths",
		"--include-paths",
		"target-N",
		"snapshot_thinning_recommended",
		"aibris clean --apfs-snapshots --force --dry-run",
		"aibris clean --apfs-snapshots --force",
		"--root <project> --category node_modules --age",
		"hard boundary",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("SKILL.md missing %q", want)
		}
	}
	if !strings.Contains(text, "--json --include-paths") {
		t.Error("agent clean --json example must carry --include-paths")
	}
	if !strings.Contains(text, "최소 버전은 **v0.12.1**") ||
		!strings.Contains(text, "`aibris --version`이 v0.12.1 미만") {
		t.Error("skill minimum-version declaration and gate must require exclusions-capable v0.12.1")
	}

	var jsonCommands []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "aibris clean ") && hasCLIFlag(line, "--json") {
			jsonCommands = append(jsonCommands, line)
		}
	}
	if len(jsonCommands) != 4 {
		t.Fatalf("JSON workflow command count = %d; want two preview/execute pairs", len(jsonCommands))
	}
	for _, command := range jsonCommands {
		if !hasCLIFlag(command, "--include-paths") {
			t.Errorf("JSON command must include --include-paths: %q", command)
		}
	}
	assertPreviewExecutePairs(t, "JSON", jsonCommands)

	var apfsCommands []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "aibris clean ") && hasCLIFlag(line, "--apfs-snapshots") {
			apfsCommands = append(apfsCommands, line)
		}
	}
	if len(apfsCommands) != 4 {
		t.Fatalf("APFS workflow command count = %d; want two preview/execute pairs", len(apfsCommands))
	}
	assertPreviewExecutePairs(t, "APFS", apfsCommands)
}

func hasCLIFlag(command, flag string) bool {
	for _, field := range strings.Fields(command) {
		if field == flag {
			return true
		}
	}
	return false
}

func assertPreviewExecutePairs(t *testing.T, kind string, commands []string) {
	t.Helper()
	if len(commands)%2 != 0 {
		t.Fatalf("%s workflow command count = %d; want preview/execute pairs", kind, len(commands))
	}
	for i := 0; i < len(commands); i += 2 {
		preview, execute := commands[i], commands[i+1]
		if !hasCLIFlag(preview, "--force") || !strings.HasSuffix(preview, " --dry-run") {
			t.Errorf("%s preview must include --force and end in --dry-run: %q", kind, preview)
		}
		if hasCLIFlag(execute, "--dry-run") {
			t.Errorf("%s execution must not include --dry-run: %q", kind, execute)
		}
		if want := strings.TrimSuffix(preview, " --dry-run"); execute != want {
			t.Errorf("%s execution must remove only --dry-run\npreview: %q\nexecute: %q", kind, preview, execute)
		}
	}
}
