package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCatalogCacheLeafPreviewMatchesPathRemovalPolicy(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, cache := range []string{"npm", "go-build", "homebrew", "uv"} {
		for _, source := range []string{"live", "prior-scan"} {
			for _, leaf := range []string{"directory", "symlink"} {
				t.Run(cache+"/"+source+"/"+leaf, func(t *testing.T) {
					if cache == "homebrew" && runtime.GOOS != "darwin" {
						t.Skip("Homebrew catalog entry is macOS-only")
					}
					home := t.TempDir()
					testutil.SetHome(t, home)
					t.Setenv("PATH", t.TempDir())
					path := filepath.Join(home, ".npm", "_cacache")
					if runtime.GOOS == "windows" {
						path = filepath.Join(home, ".cache", "npm-cache", "_cacache")
					}
					switch cache {
					case "go-build":
						path = testutil.GoBuildCache(home)
					case "homebrew":
						path = filepath.Join(home, "Library", "Caches", "Homebrew")
					case "uv":
						path = testutil.UVCache(home)
					}
					payloadPath := path
					if leaf == "symlink" {
						payloadPath = filepath.Join(home, "elsewhere", cache)
					}
					payload := strings.Repeat("x", 5000)
					writeJSONReceiptFixture(t, payloadPath, payload)
					payloadName := "payload"
					if cache == "go-build" {
						payloadName = "log.txt"
						if err := os.Rename(filepath.Join(payloadPath, "payload"), filepath.Join(payloadPath, payloadName)); err != nil {
							t.Fatal(err)
						}
					}
					if leaf == "symlink" {
						if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink(payloadPath, path); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
					}
					if source == "prior-scan" {
						if stdout, stderr, err := runCleanJSONProcess(t, binary, home, "scan", "--json", "--root", home); err != nil {
							t.Fatalf("scan = %v stdout=%s stderr=%s", err, stdout, stderr)
						}
					}
					args := []string{"clean", "--dry-run", "--no-guide", "--root", home, "--age=1h", "--pressure", "--category=build-cache,other-cache"}
					stdout, stderr, err := runCleanJSONProcess(t, binary, home, append(args, "--json")...)
					if err != nil {
						t.Fatalf("plan = %v stdout=%s stderr=%s", err, stdout, stderr)
					}
					document := decodeJSONReceiptDocument(t, stdout)
					targets := jsonReceiptArray(t, document, "physical_targets")
					rows := jsonReceiptArray(t, document, "rows")
					if len(targets) != 1 || len(rows) < 1 {
						t.Fatalf("plan inventory = %s", stdout)
					}
					refused := leaf == "symlink" && cache != "uv"
					decision := "selected"
					// The CLI already protects uv symlink leaves through scan identity
					// checks; its command eligibility remains unchanged in the domain test.
					if leaf == "symlink" && cache == "uv" {
						decision = "protected"
					}
					if refused {
						decision = "skipped"
					}
					if targets[0]["decision"] != decision || rows[0]["decision"] != decision {
						t.Errorf("decision = %v/%v; want %s", targets[0]["decision"], rows[0]["decision"], decision)
					}
					if refused {
						totals := jsonReceiptObject(t, document, "totals")
						if rows[0]["policy_decision"] != "skipped" || !strings.Contains(stdout, "cache_leaf_symlink") ||
							jsonReceiptInt(totals, "selected") != 0 || jsonReceiptInt64(totals, "selected_bytes") != 0 || jsonReceiptInt(totals, "skipped") != 1 {
							t.Errorf("refused JSON preview = %s", stdout)
						}
					} else if strings.Contains(stdout, "cache_leaf_symlink") {
						t.Errorf("ordinary path/uv command policy changed: %s", stdout)
					}
					stdout, stderr, err = runCleanJSONProcess(t, binary, home, args...)
					if err != nil {
						t.Fatalf("human preview = %v stdout=%s stderr=%s", err, stdout, stderr)
					}
					if refused && (!strings.Contains(stdout, string(cleaner.EligibilityReasonCacheLeafSymlink)) || strings.Contains(stdout, "remove-path")) {
						t.Errorf("human preview must show refusal without selecting removal: %s", stdout)
					}
					if !refused && strings.Contains(stdout, string(cleaner.EligibilityReasonCacheLeafSymlink)) {
						t.Errorf("ordinary human preview changed: %s", stdout)
					}
					if data, err := os.ReadFile(filepath.Join(payloadPath, payloadName)); err != nil || string(data) != payload {
						t.Errorf("preview mutated target: %v", err)
					}
					if leaf == "symlink" {
						if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
							t.Errorf("preview changed link: %v", err)
						}
					}
				})
			}
		}
	}
}
