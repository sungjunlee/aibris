package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanCmdLastScanReuseBySelector(t *testing.T) {
	type testCase struct {
		name         string
		first        []string
		second       []string
		seed         func(t *testing.T, workspace string)
		wantReuse    bool
		wantReason   string
		wantScanning bool
	}

	cases := []testCase{
		{
			name: "same selector dry-run then clean reuses and mentions last scan",
			first: []string{
				"clean", "--no-guide", "--dry-run", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			second: []string{
				"clean", "--no-guide", "--force", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			wantReuse: true,
		},
		{
			name: "different selector dry-run default then strip does not reuse",
			first: []string{
				"clean", "--no-guide", "--dry-run", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			second: []string{
				"clean", "--strip", "--dry-run", "--age=1h", "--root", "WORKSPACE",
			},
			wantReason:   lastScanRescanSelector,
			wantScanning: true,
		},
		{
			name: "different selector dry-run default then pressure does not reuse",
			first: []string{
				"clean", "--no-guide", "--dry-run", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			second: []string{
				"clean", "--no-guide", "--pressure", "--dry-run", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			wantReason:   lastScanRescanSelector,
			wantScanning: true,
		},
		{
			name: "stale cache prints a reason without the home path",
			second: []string{
				"clean", "--no-guide", "--dry-run", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			seed: func(t *testing.T, workspace string) {
				t.Helper()
				saveLastScanReuseFixture(t, workspace, lastScanCache{
					SchemaVersion:             lastScanCacheSchemaVersion,
					ProviderIdentity:          adapter.DefaultProviderIdentity(),
					RetentionProviderIdentity: retention.DefaultProviderIdentity(),
					Selector:                  lastScanSelectorDelete,
					CreatedAt:                 time.Now().Add(-2 * lastScanCacheMaxAge),
					Result:                    types.ScanResult{},
				})
			},
			wantReason:   lastScanRescanStale,
			wantScanning: true,
		},
		{
			name: "provider identity mismatch prints a reason without the home path",
			second: []string{
				"clean", "--no-guide", "--dry-run", "--age=1h",
				"--category=node_modules", "--root", "WORKSPACE",
			},
			seed: func(t *testing.T, workspace string) {
				t.Helper()
				saveLastScanReuseFixture(t, workspace, lastScanCache{
					SchemaVersion:             lastScanCacheSchemaVersion,
					ProviderIdentity:          adapter.DefaultProviderIdentity() + "-mismatched",
					RetentionProviderIdentity: retention.DefaultProviderIdentity(),
					Selector:                  lastScanSelectorDelete,
					CreatedAt:                 time.Now(),
					Result:                    types.ScanResult{},
				})
			},
			wantReason:   lastScanRescanProvider,
			wantScanning: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetScanFlags()
			resetCleanFlags()
			home := t.TempDir()
			testutil.SetHome(t, home)
			workspace := filepath.Join(home, "workspace")
			modules := filepath.Join(workspace, "app", "node_modules")
			if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
				t.Fatal(err)
			}
			past := time.Now().Add(-2 * time.Hour)
			if err := os.Chtimes(modules, past, past); err != nil {
				t.Fatal(err)
			}

			if tc.seed != nil {
				tc.seed(t, workspace)
			}
			if len(tc.first) > 0 {
				resetCleanFlags()
				captureOutput(func() {
					rootCmd.SetArgs(rewriteWorkspaceArgs(tc.first, workspace))
					_ = rootCmd.Execute()
				})
			}

			resetCleanFlags()
			output := captureOutput(func() {
				rootCmd.SetArgs(rewriteWorkspaceArgs(tc.second, workspace))
				_ = rootCmd.Execute()
			})

			reuseLine := lastScanHumanDecisionLine(output, "using last scan from")
			reasonLine := lastScanHumanDecisionLine(output, "scanning again:")
			assertNoHomePath(t, home, reuseLine)
			assertNoHomePath(t, home, reasonLine)

			if tc.wantReuse {
				if reuseLine == "" || !strings.Contains(reuseLine, "ago") {
					t.Fatalf("expected reuse notice mentioning last scan; got:\n%s", output)
				}
				if !strings.Contains(output, "scan    cached") {
					t.Fatalf("expected cached audit line; got:\n%s", output)
				}
				if strings.Contains(output, "scanning again:") {
					t.Fatalf("same-selector reuse printed a rescan reason:\n%s", output)
				}
				if strings.Contains(output, "scanning ") {
					t.Fatalf("same-selector reuse ran a live scan:\n%s", output)
				}
				return
			}

			if reasonLine != tc.wantReason {
				t.Fatalf("rescan reason = %q; want %q\n%s", reasonLine, tc.wantReason, output)
			}
			if strings.Contains(output, "using last scan from") {
				t.Fatalf("refused reuse still mentioned last scan:\n%s", output)
			}
			if tc.wantScanning && !strings.Contains(output, "scanning ") {
				t.Fatalf("refused reuse did not scan again:\n%s", output)
			}
			if strings.Contains(output, "scan    cached") {
				t.Fatalf("refused reuse still reported a cached scan:\n%s", output)
			}
		})
	}
}

func TestLastScanCacheReuseDecisionReasons(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	resolved, err := scanner.NormalizeRoots([]string{workspace})
	if err != nil {
		t.Fatal(err)
	}

	valid := lastScanCache{
		SchemaVersion:             lastScanCacheSchemaVersion,
		ProviderIdentity:          adapter.DefaultProviderIdentity(),
		RetentionProviderIdentity: retention.DefaultProviderIdentity(),
		Selector:                  lastScanSelectorDelete,
		CreatedAt:                 time.Now(),
		Roots:                     resolved,
		Result:                    types.ScanResult{},
	}

	cases := []struct {
		name     string
		mutate   func(cache lastScanCache) lastScanCache
		excludes []string
		strip    bool
		want     string
	}{
		{
			name: "provider set changed",
			mutate: func(cache lastScanCache) lastScanCache {
				cache.ProviderIdentity = adapter.DefaultProviderIdentity() + "-other"
				return cache
			},
			want: lastScanRescanProvider,
		},
		{
			name: "cache stale",
			mutate: func(cache lastScanCache) lastScanCache {
				cache.CreatedAt = time.Now().Add(-2 * lastScanCacheMaxAge)
				return cache
			},
			want: lastScanRescanStale,
		},
		{
			name: "selector mismatch",
			mutate: func(cache lastScanCache) lastScanCache {
				cache.Selector = lastScanSelectorDelete
				return cache
			},
			strip: true,
			want:  lastScanRescanSelector,
		},
		{
			name: "schema mismatch",
			mutate: func(cache lastScanCache) lastScanCache {
				cache.SchemaVersion = lastScanCacheSchemaVersion + 1
				return cache
			},
			want: lastScanRescanSchema,
		},
		{
			name: "roots mismatch",
			mutate: func(cache lastScanCache) lastScanCache {
				cache.Roots = []string{filepath.Join(home, "other")}
				return cache
			},
			want: lastScanRescanRoots,
		},
		{
			name: "incomplete",
			mutate: func(cache lastScanCache) lastScanCache {
				cache.Result.ProviderErrors = []types.ScanProviderError{{
					Tool:    types.ToolCodex,
					Message: "boom",
				}}
				return cache
			},
			want: lastScanRescanIncomplete,
		},
		{
			name: "exclusions on current command",
			mutate: func(cache lastScanCache) lastScanCache {
				return cache
			},
			excludes: []string{"node_modules"},
			want:     lastScanRescanExclusions,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCleanFlags()
			cleanStrip = tc.strip
			cache := tc.mutate(valid)
			if err := saveLastScanCache(cache); err != nil {
				t.Fatal(err)
			}
			_, _, reason := lastScanCacheReuseDecision(resolved, tc.excludes)
			if reason != tc.want {
				t.Fatalf("reason = %q; want %q", reason, tc.want)
			}
			assertNoHomePath(t, home, reason)
		})
	}
}

func saveLastScanReuseFixture(t *testing.T, workspace string, cache lastScanCache) {
	t.Helper()
	resolved, err := scanner.NormalizeRoots([]string{workspace})
	if err != nil {
		t.Fatal(err)
	}
	cache.Roots = resolved
	if err := saveLastScanCache(cache); err != nil {
		t.Fatal(err)
	}
}

func rewriteWorkspaceArgs(args []string, workspace string) []string {
	out := append([]string(nil), args...)
	for i, arg := range out {
		if arg == "WORKSPACE" {
			out[i] = workspace
		}
	}
	return out
}

func lastScanHumanDecisionLine(output, prefix string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func assertNoHomePath(t *testing.T, home, line string) {
	t.Helper()
	if line == "" {
		return
	}
	if strings.Contains(line, home) {
		t.Fatalf("human line includes home path %q: %q", home, line)
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home && strings.Contains(line, resolved) {
		t.Fatalf("human line includes resolved home path %q: %q", resolved, line)
	}
}
