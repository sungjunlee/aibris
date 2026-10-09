//go:build windows

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestWindowsCatalogCacheLeafPreviewRefusesJunction(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, source := range []string{"live", "prior-scan"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := filepath.Join(home, ".cache", "npm-cache", "_cacache")
			target := filepath.Join(home, "elsewhere")
			payload := strings.Repeat("x", 5000)
			writeJSONReceiptFixture(t, target, payload)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			testutil.WindowsJunction(t, path, target)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Fixture creation is complete; no package manager can run.
			t.Setenv("PATH", t.TempDir())
			if source == "prior-scan" {
				if stdout, stderr, err := runCleanJSONProcess(t, binary, home, "scan", "--json", "--root", home); err != nil {
					t.Fatalf("scan = %v stdout=%s stderr=%s", err, stdout, stderr)
				}
			}
			args := []string{"clean", "--dry-run", "--no-guide", "--root", home, "--age=1h", "--pressure", "--category=build-cache"}
			stdout, stderr, err := runCleanJSONProcess(t, binary, home, append(args, "--json")...)
			if err != nil {
				t.Fatalf("plan = %v stdout=%s stderr=%s", err, stdout, stderr)
			}
			document := decodeJSONReceiptDocument(t, stdout)
			targets := jsonReceiptArray(t, document, "physical_targets")
			rows := jsonReceiptArray(t, document, "rows")
			if len(targets) != 1 || len(rows) != 1 {
				t.Fatalf("plan inventory = %s", stdout)
			}
			totals := jsonReceiptObject(t, document, "totals")
			if targets[0]["decision"] != "skipped" || rows[0]["decision"] != "skipped" || rows[0]["policy_decision"] != "skipped" ||
				!strings.Contains(stdout, "cache_leaf_symlink") || jsonReceiptInt(totals, "selected") != 0 ||
				jsonReceiptInt64(totals, "selected_bytes") != 0 || jsonReceiptInt(totals, "skipped") != 1 ||
				jsonReceiptInt64(totals, "physical_bytes") != 5000 {
				t.Errorf("junction JSON preview = %s", stdout)
			}
			stdout, stderr, err = runCleanJSONProcess(t, binary, home, args...)
			if err != nil {
				t.Fatalf("human preview = %v stdout=%s stderr=%s", err, stdout, stderr)
			}
			if !strings.Contains(stdout, string(cleaner.EligibilityReasonCacheLeafSymlink)) || strings.Contains(stdout, "remove-path") {
				t.Errorf("human preview must show refusal: %s", stdout)
			}
			if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeIrregular == 0 || !os.SameFile(before, info) {
				t.Errorf("preview changed junction: %v", err)
			}
			for _, dir := range []string{path, target} {
				if data, err := os.ReadFile(filepath.Join(dir, "payload")); err != nil || string(data) != payload {
					t.Errorf("preview changed payload at %q: %v", dir, err)
				}
			}
		})
	}
}
