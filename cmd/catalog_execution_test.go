package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCleanGoCachePathRemovalFreshAndCached(t *testing.T) {
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
			if err := os.Rename(filepath.Join(path, "payload"), filepath.Join(path, "log.txt")); err != nil {
				t.Fatal(err)
			}
			chtimesTree(t, path, time.Now().Add(-48*time.Hour))

			outside := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			binDir := t.TempDir()
			marker := filepath.Join(home, "go-invoked")
			writeJSONReceiptExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nprintf invoked > '"+marker+"'\n")
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
			if _, err := os.Stat(filepath.Join(path, "log.txt")); err != nil {
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
				t.Errorf("Go path removal accounting: %s", stdout)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Errorf("Go executable ran: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("cache root remains: %v", err)
			}
			if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
				t.Errorf("outside sentinel changed: %q %v", data, err)
			}
		})
	}
}

func TestCleanGoCacheForeignEntryPreservesUserData(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, source := range []string{"live", "cached"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := testutil.GoBuildCache(home)
			for _, name := range []string{"README", "trim.txt", "00/artifact"} {
				file := filepath.Join(path, name)
				if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
					t.Fatal(err)
				}
				content := "cache data"
				if name == "README" {
					content = "This directory holds cached build artifacts from the Go build system."
				}
				if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if source == "cached" {
				if stdout, stderr, err := runCleanJSONProcess(t, binary, home, "scan", "--json", "--root", path); err != nil {
					t.Fatalf("scan = %v stdout=%s stderr=%s", err, stdout, stderr)
				}
			}
			// Foreign user data arrives after the cached scan. --pressure models
			// cache-age relaxation without depending on host disk fullness.
			if err := os.MkdirAll(filepath.Join(path, "project", ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(path, "personal.txt"), []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := runCleanJSONProcess(t, binary, home, "clean", "--json", "--force", "--no-guide",
				"--root", path, "--age=7d", "--pressure", "--category=build-cache")
			if err != nil && source == "live" {
				t.Fatalf("clean = %v stdout=%s stderr=%s", err, stdout, stderr)
			}
			document := decodeJSONReceiptDocument(t, stdout)
			// Cached inventory may retain a selected claim; execution must then
			// refuse it rather than silently authorize the shared directory.
			if err != nil && document["status"] != "failed" {
				t.Errorf("cached drift did not fail closed: %s", stdout)
			}
			if document["schema_version"] != float64(1) || jsonReceiptInt64(jsonReceiptObject(t, document, "totals"), "freed_bytes") != 0 {
				t.Errorf("shared directory cleanup credited removal: %s", stdout)
			}
			if data, err := os.ReadFile(filepath.Join(path, "personal.txt")); err != nil || string(data) != "keep" {
				t.Errorf("user data changed: %q, %v", data, err)
			}
			for _, name := range []string{"README", "trim.txt", "00/artifact", "project/.git"} {
				if _, err := os.Lstat(filepath.Join(path, name)); err != nil {
					t.Errorf("shared directory entry %s removed: %v", name, err)
				}
			}
		})
	}
}
