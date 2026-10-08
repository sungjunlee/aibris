package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCodexTmpPublicScanAndCleanRemainUnsupported(t *testing.T) {
	for _, relocated := range []bool{false, true} {
		name := "default-home"
		if relocated {
			name = "relocated-home"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			codexHome := filepath.Join(home, ".codex")
			if relocated {
				codexHome = filepath.Join(home, "custom-codex")
				t.Setenv("CODEX_HOME", codexHome)
			}
			contents := []byte("fixture tmp content")
			var files []string
			for _, rel := range []string{
				"path/codex-arg0/applypatch",
				"path/codex-arg0/apply_patch",
				"unknown-child/residue",
			} {
				path := filepath.Join(codexHome, "tmp", filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, contents, 0644); err != nil {
					t.Fatal(err)
				}
				files = append(files, path)
			}

			result, err := scanner.Scan(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.TotalCount != 0 {
				t.Fatalf("tmp-only fixture surfaced debris: %+v", result)
			}

			resetCleanFlags()
			t.Cleanup(resetCleanFlags)
			output := captureOutput(func() {
				rootCmd.SetArgs([]string{
					"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
					"--force", "--risky", "--age=1ns",
				})
				if err := rootCmd.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			plan := parseCleanJSONPlan(t, output)
			if len(plan.PhysicalTargets) != 0 || plan.Totals.Selected != 0 {
				t.Fatalf("tmp-only fixture became a cleanup candidate: %s", output)
			}
			for _, path := range files {
				got, err := os.ReadFile(path)
				if err != nil || string(got) != string(contents) {
					t.Fatalf("tmp content changed at %s: %q, %v", path, got, err)
				}
			}
		})
	}
}
