package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPrintJSONRetentionIsSeparateAndLeavesExistingShapesInvariant(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	result := &types.ScanResult{
		Worktrees: []types.DebrisInfo{{
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			ID:       "ordinary",
			Path:     "/tmp/node_modules",
			Size:     10,
			ModTime:  now,
		}},
		TotalCount: 1,
		TotalSize:  10,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryNodeModules: {Count: 1, Size: 10},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolNodeModules: {Count: 1, Size: 10},
		},
	}
	baseline := decodeJSONOutput(t, captureOutput(func() { printJSON(result) }))

	result.Retention = types.RetentionProjection{
		Buckets: []types.RetentionBucket{{
			StoreID:       types.RetentionStoreCodexSessions,
			BucketID:      "2025-12",
			UnitCount:     2,
			MemberCount:   2,
			ApparentBytes: 1000,
			OrphanedCount: 1,
			OrphanedBytes: 400,
		}},
	}
	withRetention := decodeJSONOutput(t, captureOutput(func() { printJSON(result) }))

	if !reflect.DeepEqual(baseline.Worktrees, withRetention.Worktrees) ||
		!reflect.DeepEqual(baseline.Summary, withRetention.Summary) {
		t.Fatalf("retention changed existing JSON semantics:\nbaseline=%+v\nwith=%+v",
			baseline, withRetention)
	}
	if len(withRetention.Retention.Buckets) != 1 {
		t.Fatalf("retention buckets = %+v; want one", withRetention.Retention.Buckets)
	}
	bucket := withRetention.Retention.Buckets[0]
	if bucket.StoreID != "codex-sessions" || bucket.BucketID != "2025-12" ||
		bucket.UnitCount != 2 || bucket.OrphanedCount != 1 {
		t.Fatalf("bucket = %+v", bucket)
	}
	if withRetention.Retention.Partial {
		t.Fatalf("retention partial = true; want false")
	}
}

func TestPrintHumanRetentionProjectionDoesNotExposePrivateMemberEvidence(t *testing.T) {
	projection := types.RetentionProjection{
		Buckets: []types.RetentionBucket{{
			StoreID:       types.RetentionStoreCodexSessions,
			BucketID:      "2026-03",
			UnitCount:     3,
			MemberCount:   3,
			ApparentBytes: 9000,
			OrphanedCount: 1,
			OrphanedBytes: 3000,
		}},
	}
	output := captureOutput(func() { printRetentionProjection(projection) })
	for _, want := range []string{"retention (protected content, read-only)", "2026-03", "units 3", "orphaned 1"} {
		if !strings.Contains(output, want) {
			t.Fatalf("human output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "session-private") || strings.Contains(output, ".jsonl") {
		t.Fatalf("human output leaked private member evidence:\n%s", output)
	}
}

func TestPrintHumanRetentionProjectionPartialIsPathFree(t *testing.T) {
	projection := types.RetentionProjection{
		Partial: true,
		ProviderErrors: []types.RetentionProviderError{{
			StoreID: types.RetentionStoreCodexSessions,
			Message: "reading store: permission denied or unreadable store subtree",
		}},
	}
	output := captureOutput(func() { printRetentionProjection(projection) })
	if !strings.Contains(output, "completeness partial (retention inventory only)") {
		t.Fatalf("human output missing partial marker:\n%s", output)
	}
	if strings.Contains(output, ".codex") || strings.Contains(output, ".jsonl") {
		t.Fatalf("human output leaked store path components:\n%s", output)
	}
}

func TestRetentionInventoryNeverCreatesCleanupCandidates(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	store := filepath.Join(home, ".codex", "sessions", "2026", "01", "01")
	if err := os.MkdirAll(store, 0755); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(store, "rollout-inventory.jsonl")
	goneCWD := filepath.Join(home, "gone-project")
	if err := os.MkdirAll(goneCWD, 0755); err != nil {
		t.Fatal(err)
	}
	payload := `{"timestamp":"2099-12-31T23:59:59Z","type":"session_meta","payload":{"id":"session-private","originator":"codex_cli_rs","cli_version":"1.2.3","cwd":"` + goneCWD + `"}}` + "\n"
	if err := os.WriteFile(leaf, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(leaf, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(goneCWD); err != nil {
		t.Fatal(err)
	}

	roots, err := scanner.NormalizeRoots([]string{home})
	if err != nil {
		t.Fatal(err)
	}
	result, err := scanner.ScanWithOptions(t.Context(), types.ScanOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Retention.Buckets) != 1 || result.Retention.Buckets[0].BucketID != "2026-01" {
		t.Fatalf("retention buckets = %+v; want 2026-01", result.Retention.Buckets)
	}
	if result.Retention.Buckets[0].OrphanedCount != 1 {
		t.Fatalf("bucket = %+v; absent cwd should be orphaned in the inventory", result.Retention.Buckets[0])
	}
	if result.TotalCount != 0 {
		t.Fatalf("inventory entries became cleanup candidates: %+v", result.Worktrees)
	}
	if result.Partial() {
		t.Fatalf("result partial; retention must not affect debris completeness")
	}
}

func TestRetentionPartialDoesNotBlockOrdinaryCleanPrerequisite(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:        types.ToolBuildCache,
		Category:    types.CategoryBuildCache,
		ID:          "cache",
		Path:        filepath.Join(home, ".cache", "go-build"),
		Size:        8,
		ModTime:     time.Now().Add(-48 * time.Hour),
		CleanupKind: types.CleanupRemovePath,
	}
	// Cache adapters always record the path's own mtime; a cached entry without
	// it is refused, so the fixture has to carry it too.
	item.PathModTime = item.ModTime
	if err := os.MkdirAll(item.Path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(item.Path, item.ModTime, item.ModTime); err != nil {
		t.Fatal(err)
	}
	evidence, err := captureLastScanTargetEvidence([]types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	result := types.ScanResult{
		Worktrees:  []types.DebrisInfo{item},
		TotalCount: 1,
		TotalSize:  item.Size,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryBuildCache: {Count: 1, Size: item.Size},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolBuildCache: {Count: 1, Size: item.Size},
		},
		Retention: types.RetentionProjection{
			Buckets:        []types.RetentionBucket{},
			Partial:        true,
			ProviderErrors: []types.RetentionProviderError{{StoreID: types.RetentionStoreCodexSessions, Message: "reading store: permission denied"}},
		},
	}
	if result.Partial() {
		t.Fatalf("ScanResult.Partial() must ignore retention partiality")
	}
	if err := saveLastScanCache(lastScanCache{
		SchemaVersion:             lastScanCacheSchemaVersion,
		ProviderIdentity:          adapter.DefaultProviderIdentity(),
		RetentionProviderIdentity: retention.DefaultProviderIdentity(),
		CreatedAt:                 time.Now(),
		Roots:                     []string{resolvedHome},
		Result:                    result,
		TargetEvidence:            evidence,
	}); err != nil {
		t.Fatal(err)
	}
	cached, _, ok := readFreshLastScanCache([]string{resolvedHome})
	if !ok {
		t.Fatal("readFreshLastScanCache() rejected cache with partial retention")
	}
	if len(cached.Worktrees) != 1 {
		t.Fatalf("cached worktrees = %d; want 1", len(cached.Worktrees))
	}
	if !cached.Retention.Partial {
		t.Fatalf("cached retention partial = false; want true preserved")
	}
}

// decodeJSONOutput mirrors the jsonOutput shape for assertions.
func decodeJSONOutput(t *testing.T, raw string) jsonOutput {
	t.Helper()
	var out jsonOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
