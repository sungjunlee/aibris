package cleaner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFakeCommandEnvDoesNotSkipCleanerSuite(t *testing.T) {
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(filepath.Base(bin), ".exe")
	if !strings.HasSuffix(name, ".test") {
		t.Fatalf("test binary %q is not a .test executable", bin)
	}

	clean := exec.Command(bin, "-test.run=^TestExecuteRefusesUntrustedCleanupRecipes$", "-test.count=1", "-test.v", "-test.timeout=3m")
	clean.Env = testBinaryEnv()
	cleanOut, cleanErr := clean.CombinedOutput()
	if cleanErr != nil || !strings.Contains(string(cleanOut), "=== RUN   TestExecuteRefusesUntrustedCleanupRecipes") || !strings.Contains(string(cleanOut), "--- PASS: TestExecuteRefusesUntrustedCleanupRecipes") {
		t.Fatalf("clean subprocess error = %v\n%s", cleanErr, cleanOut)
	}

	polluted := exec.Command(bin, "-test.run=^TestExecuteRefusesUntrustedCleanupRecipes$", "-test.count=1", "-test.v", "-test.timeout=3m")
	polluted.Env = testBinaryEnv("AIBRIS_FAKE_CMD=exit", "AIBRIS_FAKE_CMD_EXIT=0")
	pollutedOut, pollutedErr := polluted.CombinedOutput()
	if pollutedErr == nil {
		t.Fatalf("polluted subprocess succeeded without running tests:\n%s", pollutedOut)
	}
	text := string(pollutedOut)
	if strings.Contains(text, "=== RUN") || strings.Contains(text, "--- PASS:") || !strings.Contains(text, "AIBRIS_FAKE_CMD=exit") || !strings.Contains(text, "skip the suite") {
		t.Fatalf("polluted subprocess error = %v\n%s", pollutedErr, pollutedOut)
	}
}

func testBinaryEnv(extra ...string) []string {
	skip := map[string]bool{}
	for _, entry := range extra {
		key, _, _ := strings.Cut(entry, "=")
		skip[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AIBRIS_FAKE_CMD") || skip[key] {
			continue
		}
		env = append(env, entry)
	}
	return append(env, extra...)
}
