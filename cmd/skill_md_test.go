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
}
