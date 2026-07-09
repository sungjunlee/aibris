package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestCleanCmd_ForceKeepsClassicRoute(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	saveUsefulGuidedCleanFixture(t, home, "hash-force-classic", time.Now().Add(-48*time.Hour))
	defer withStdin(t, "")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--force"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "guided codex worktree cleanup") {
		t.Fatalf("--force should keep classic cleanup route; got: %s", output)
	}
	if !strings.Contains(output, "scan summary") {
		t.Fatalf("classic output missing scan summary; got: %s", output)
	}
}

func TestCleanCmd_GuideOverridesForceSelector(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	saveUsefulGuidedCleanFixture(t, home, "hash-guide-force-override", time.Now().Add(-48*time.Hour))
	defer withStdin(t, "\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--force", "--guide"})
		rootCmd.Execute()
	})

	for _, want := range []string{"guided codex worktree cleanup", "reason     requested by --guide"} {
		if !strings.Contains(output, want) {
			t.Fatalf("--guide should override --force selector; missing %q in: %s", want, output)
		}
	}
}
