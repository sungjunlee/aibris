package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fileSHA256(empty)
	if err != nil {
		t.Fatal(err)
	}
	const emptySHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != emptySHA {
		t.Fatalf("empty sha = %s; want %s", got, emptySHA)
	}

	content := []byte("aibris-perfharness\n")
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, content, 0o644); err != nil {
		t.Fatal(err)
	}
	got2, err := fileSHA256(f)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if want := hex.EncodeToString(sum[:]); got2 != want {
		t.Fatalf("sha = %s; want %s", got2, want)
	}
}

// repoRoot resolves the enclosing git repository root from the test's working
// directory (tools/perfharness).
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not in a git repository: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// TestBuildBinaryIntegration exports HEAD, builds it, and confirms the produced
// binary scans an empty synthetic home and exits zero. Gated behind
// PERFHARNESS_INTEGRATION=1 because it compiles aibris (slow).
func TestBuildBinaryIntegration(t *testing.T) {
	if os.Getenv("PERFHARNESS_INTEGRATION") == "" {
		t.Skip("set PERFHARNESS_INTEGRATION=1 to run the binary-build integration test")
	}
	repo := repoRoot(t)
	out := t.TempDir()
	bin, err := BuildBinary("base", "HEAD", repo, out)
	if err != nil {
		t.Fatalf("BuildBinary: %v", err)
	}
	if len(bin.SHA256) != 64 {
		t.Fatalf("binary sha = %q; want 64 hex chars", bin.SHA256)
	}
	if bin.SourceSHA == "" {
		t.Fatalf("missing source sha")
	}
	if fi, err := os.Stat(bin.Path); err != nil || fi.Size() == 0 {
		t.Fatalf("built binary missing/empty: err=%v", err)
	}

	// The built binary must scan an empty home and emit parseable JSON, exit 0.
	home := t.TempDir()
	cmd := exec.Command(bin.Path, "scan", "--json")
	cmd.Env = append(os.Environ(), "HOME="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("built binary scan failed: %v\nstderr=%s", err, stderr.String())
	}
	if _, err := parseScanOutput(stdout.Bytes()); err != nil {
		t.Fatalf("built binary emitted unparseable scan output: %v", err)
	}
}
