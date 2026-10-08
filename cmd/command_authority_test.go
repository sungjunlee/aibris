package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCachedCleanupRecipeRefusalAccountsPartialSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture is Unix-specific")
	}
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	testutil.SetHome(t, home)
	cachePath := testutil.GoBuildCache(home)
	modules := filepath.Join(home, "workspace", "project", "node_modules")
	writeJSONReceiptFixture(t, cachePath, "cache payload")
	writeJSONReceiptFixture(t, modules, "modules payload")
	if stdout, stderr, err := runCleanJSONProcess(t, binary, home, "scan", "--json", "--root", home); err != nil {
		t.Fatalf("scan = %v stdout=%s stderr=%s", err, stdout, stderr)
	}
	cache, ok := readLastScanCache()
	if !ok {
		t.Fatal("scan cache missing")
	}
	outside := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	writeJSONReceiptExecutable(t, filepath.Join(binDir, "untrusted"), "#!/bin/sh\nprintf changed > '"+outside+"'\n")
	t.Setenv("PATH", binDir)
	found := false
	for i := range cache.Result.Worktrees {
		if cache.Result.Worktrees[i].ID == "go-build" {
			cache.Result.Worktrees[i].CleanupCommand = []string{"untrusted"}
			found = true
		}
	}
	if !found {
		t.Fatal("scan did not inventory go-build")
	}
	if err := saveLastScanCache(cache); err != nil {
		t.Fatal(err)
	}
	// Prove identity validation accepts the altered inventory; authority must
	// be re-derived at execution rather than depending on a rescan.
	if _, _, reason, ok := inspectLastScanCache(cache.Roots, "delete", true); !ok {
		t.Fatalf("tampered inventory was not reusable: %s", reason)
	}
	stdout, stderr, err := runCleanJSONProcess(t, binary, home, "clean", "--json", "--force", "--no-guide",
		"--root", home, "--age=1h", "--category=build-cache,node_modules")
	if err == nil {
		t.Errorf("tampered command succeeded: stdout=%s stderr=%s", stdout, stderr)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	totals := jsonReceiptObject(t, document, "totals")
	if document["status"] != "partial_failure" || jsonReceiptInt(totals, "requested") != 2 ||
		jsonReceiptInt(totals, "removed") != 1 || jsonReceiptInt(totals, "failed") != 1 ||
		jsonReceiptInt(totals, "partial") != 0 || jsonReceiptInt(totals, "cancelled") != 0 {
		t.Errorf("refusal accounting = status %v totals %+v", document["status"], totals)
	}
	found = false
	for _, target := range jsonReceiptArray(t, document, "physical_targets") {
		if target["state"] == "failed" {
			codes, _ := target["reason_codes"].([]any)
			for _, code := range codes {
				found = found || code == "cleanup_recipe_changed"
			}
			if jsonReceiptInt64(target, "freed_bytes") != 0 || target["physical_removed"] != false {
				t.Errorf("refused target credited mutation: %+v", target)
			}
		}
	}
	if !found {
		t.Errorf("receipt lacks stable cleanup_recipe_changed refusal: %s", stdout)
	}
	if strings.Contains(stdout, home) || strings.Contains(stderr, home) {
		t.Error("redacted receipt leaked home")
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("refused cache changed: %v", err)
	}
	if _, err := os.Stat(modules); !os.IsNotExist(err) {
		t.Errorf("successful owner was not removed: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Errorf("outside sentinel changed: %q %v", data, err)
	}
}
