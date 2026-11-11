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
		"aibris clean --apfs-snapshots --dry-run",
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
		if strings.HasPrefix(line, "aibris clean ") &&
			strings.Contains(line, "--json") &&
			strings.Contains(line, "--include-paths") {
			jsonCommands = append(jsonCommands, line)
		}
	}
	if len(jsonCommands) != 4 {
		t.Fatalf("JSON workflow command count = %d; want two preview/execute pairs", len(jsonCommands))
	}
	for i := 0; i < len(jsonCommands); i += 2 {
		preview, execute := jsonCommands[i], jsonCommands[i+1]
		if !strings.Contains(preview, " --force") || !strings.HasSuffix(preview, " --dry-run") {
			t.Errorf("JSON preview must include --force and end in --dry-run: %q", preview)
		}
		if strings.Contains(execute, " --dry-run") {
			t.Errorf("JSON execution must not include --dry-run: %q", execute)
		}
		if want := strings.TrimSuffix(preview, " --dry-run"); execute != want {
			t.Errorf("JSON execution must remove only --dry-run\npreview: %q\nexecute: %q", preview, execute)
		}
	}
}
