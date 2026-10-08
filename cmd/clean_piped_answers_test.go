package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanPipedAnswers(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, tt := range []struct {
		name        string
		answers     string
		mixed       bool
		interactive bool
		wantRemoved int
	}{
		{name: "guided-final", answers: "1\n\nY\n", wantRemoved: 1},
		{name: "guided-interactive", answers: "\nn\ny\n", interactive: true, wantRemoved: 1},
		{name: "guided-unified-final", answers: "\n\nY\n", mixed: true, wantRemoved: 3},
		{name: "guided-unified-interactive", answers: "\n\nn\ny\nn\n", mixed: true, interactive: true, wantRemoved: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			t.Cleanup(resetCleanFlags)
			home := t.TempDir()
			testutil.SetHome(t, home)
			paths := saveGuidedReceiptCleanFixture(t, home, "piped", 2)
			args := []string{"clean", "--guide"}
			if tt.mixed {
				modules := filepath.Join(home, "workspace", "app", "node_modules")
				writeJSONReceiptFixture(t, modules, "sentinel")
				old := time.Now().Add(-30 * 24 * time.Hour)
				chtimesTree(t, modules, old)
				appendCleanCacheItem(t, types.DebrisInfo{
					Path: modules, ID: "modules", Tool: types.ToolNodeModules,
					Category: types.CategoryNodeModules, ModTime: old, Size: 64,
				})
				paths = append(paths, modules)
				args = append(args, "--category=worktree,node_modules")
			}
			if tt.interactive {
				args = append(args, "--interactive")
			}
			// Supply every answer before any prompt reads: a scanner may read
			// the whole pipe in one call, so prompt-by-prompt writers miss #606.
			stdout, stderr, err := runCleanJSONProcessWithInput(t, binary, home, tt.answers, args...)
			if err != nil {
				t.Fatalf("clean: %v; stdout=%s; stderr=%s", err, stdout, stderr)
			}
			removed := 0
			for _, path := range paths {
				if _, err := os.Stat(path); os.IsNotExist(err) {
					removed++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if removed != tt.wantRemoved {
				t.Fatalf("removed %d targets; want %d; stdout=%s; stderr=%s", removed, tt.wantRemoved, stdout, stderr)
			}
			if tt.interactive {
				if got := strings.Count(stdout, "Remove? [y/N]: "); got != len(paths) {
					t.Fatalf("per-item prompts=%d; want %d; stdout=%s", got, len(paths), stdout)
				}
			} else if !strings.Contains(stdout, "Proceed? [y/N]: ") {
				t.Fatalf("final confirmation missing: %s", stdout)
			}
		})
	}
}
