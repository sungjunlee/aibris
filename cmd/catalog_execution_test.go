package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCleanCatalogCommandFreshAndCached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture is Unix-specific")
	}
	binary := buildCLIContractBinary(t)
	for _, source := range []string{"live", "cached"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := testutil.GoBuildCache(home)
			writeJSONReceiptFixture(t, path, "cache payload")
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			binDir := t.TempDir()
			writeJSONReceiptExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\n"+
				"if [ \"$GOCACHE\" != '"+canonical+"' ] || [ \"$*\" != 'clean -cache' ]; then\n"+
				"  printf changed > '"+outside+"'\n  exit 7\nfi\n/bin/rm -f \"$GOCACHE/payload\"\n")
			t.Setenv("PATH", binDir)
			if source == "cached" {
				if stdout, stderr, err := runCleanJSONProcess(t, binary, home, "scan", "--json", "--root", home); err != nil {
					t.Fatalf("scan = %v stdout=%s stderr=%s", err, stdout, stderr)
				}
			}
			// A dry-run must not start even a valid catalog executable.
			stdout, stderr, err := runCleanJSONProcess(t, binary, home, "clean", "--dry-run", "--json", "--no-guide",
				"--root", home, "--age=1h", "--category=build-cache")
			if err != nil {
				t.Fatalf("dry-run = %v stdout=%s stderr=%s", err, stdout, stderr)
			}
			if _, err := os.Stat(filepath.Join(path, "payload")); err != nil {
				t.Fatalf("dry-run mutated payload: %v", err)
			}
			// Dry-run persists inventory, so invalidate it to exercise live
			// execution as well as cached execution.
			if source == "live" {
				invalidateLastScanCache()
			}
			stdout, stderr, err = runCleanJSONProcess(t, binary, home, "clean", "--json", "--force", "--no-guide",
				"--root", home, "--age=1h", "--category=build-cache")
			if err != nil {
				t.Fatalf("clean = %v stdout=%s stderr=%s", err, stdout, stderr)
			}
			document := decodeJSONReceiptDocument(t, stdout)
			plan := jsonReceiptObject(t, document, "plan")
			if evidence := jsonReceiptObject(t, plan, "evidence"); evidence["source"] != source {
				t.Errorf("execution source = %v; want %s", evidence["source"], source)
			}
			if document["status"] != "succeeded" || jsonReceiptInt64(jsonReceiptObject(t, document, "totals"), "freed_bytes") != int64(len("cache payload")) {
				t.Errorf("valid command accounting: %s", stdout)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
				t.Errorf("outside sentinel changed: %q %v", data, err)
			}
		})
	}
}
