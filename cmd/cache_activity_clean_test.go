package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

func skipAgeGateIfVolumeRelaxesCaches(t *testing.T, home string) {
	t.Helper()
	report, err := volume.Inspect(home)
	if err != nil {
		return
	}
	if report.Band == volume.BandCritical {
		t.Skip("age-gate CLI contract is not isolated from automatic cache-age relaxation on a critical volume")
	}
}

// writeCacheActivityFixture builds a gradle cache whose container is
// containerAge old and whose single nested file is nestedAge old. Only the
// nested file carries the newer signal, which is exactly the shape issue #213
// is about.
func writeCacheActivityFixture(t *testing.T, home string, containerAge, nestedAge time.Duration) (string, string) {
	t.Helper()
	cache := filepath.Join(home, ".gradle", "caches")
	deep := filepath.Join(cache, "modules-2", "files-2.1")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(deep, "artifact.bin")
	if err := os.WriteFile(nested, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	chtimesTree(t, cache, time.Now().Add(-containerAge))
	nestedModTime := time.Now().Add(-nestedAge)
	if err := os.Chtimes(nested, nestedModTime, nestedModTime); err != nil {
		t.Fatal(err)
	}
	return cache, nested
}

func cacheActivityBuildCacheRow(t *testing.T, document cleanJSONPlan) cleanJSONRow {
	t.Helper()
	var rows []cleanJSONRow
	for _, row := range document.Rows {
		if row.Category == string(types.CategoryBuildCache) {
			rows = append(rows, row)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("build-cache rows = %+v; want exactly one", rows)
	}
	return rows[0]
}

// TestCleanJSONCLIContractLiveNestedCacheRefusedByAgeGate covers blocker 1 and
// blocker 2 together: the container is well past --age but the tree underneath
// is live, so the cache must be refused, and refused by the age gate rather
// than by a scan-evidence integrity error.
func TestCleanJSONCLIContractLiveNestedCacheRefusedByAgeGate(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	skipAgeGateIfVolumeRelaxesCaches(t, home)
	cache, _ := writeCacheActivityFixture(t, home, 30*24*time.Hour, 5*time.Minute)

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--category=build-cache", "--age=7d", "--root", home)
	if err != nil {
		t.Fatalf("live nested cache clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("live nested cache clean JSON stderr = %q", stderr)
	}
	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("live nested cache JSON is invalid: %v\n%s", err, stdout)
	}
	if document.Totals.Selected != 0 || document.Totals.Skipped != 1 {
		t.Fatalf("live nested cache was selected for removal: totals=%+v", document.Totals)
	}
	for _, target := range document.PhysicalTargets {
		if target.Decision == cleanJSONDecisionSelected {
			t.Fatalf("live nested cache physical target = %+v; want no selected target", target)
		}
	}
	row := cacheActivityBuildCacheRow(t, document)
	if slices.Contains(row.ReasonCodes, "scan_evidence_unavailable") {
		t.Fatalf("live nested cache refused by an integrity error instead of the age gate: %+v", row)
	}
	if !slices.Contains(row.ReasonCodes, "minimum_age") {
		t.Fatalf("live nested cache reason codes = %v; want minimum_age", row.ReasonCodes)
	}
	if _, statErr := os.Lstat(cache); statErr != nil {
		t.Fatalf("dry-run mutated the live cache: %v", statErr)
	}
}

// TestRefreshCleanupInventoryMetadataOnlyRaisesDerivedActivity pins blocker 2:
// the clean-path refresh must not replace an activity-derived ModTime with the
// container's own mtime, and must still pick up a newer container mtime.
func TestRefreshCleanupInventoryMetadataOnlyRaisesDerivedActivity(t *testing.T) {
	home := t.TempDir()
	container := filepath.Join(home, ".gradle", "caches")
	if err := os.MkdirAll(container, 0o755); err != nil {
		t.Fatal(err)
	}
	containerModTime := time.Now().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(container, containerModTime, containerModTime); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-90 * 24 * time.Hour)
	recent := time.Now().Add(-5 * time.Minute)

	items := []types.DebrisInfo{
		{
			Tool:        types.ToolBuildCache,
			Category:    types.CategoryBuildCache,
			Path:        container,
			ModTime:     recent,
			PathModTime: containerModTime,
		},
		{
			Tool:        types.ToolBuildCache,
			Category:    types.CategoryBuildCache,
			Path:        container,
			ModTime:     stale,
			PathModTime: stale,
		},
		{
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			Path:     container,
			ModTime:  recent,
		},
	}
	refreshCleanupInventoryMetadata(items)

	if !items[0].ModTime.Equal(recent) {
		t.Errorf("derived activity was lowered to %v; want the scan's %v", items[0].ModTime, recent)
	}
	if !items[0].PathModTime.Equal(containerModTime) {
		t.Errorf("derived PathModTime = %v; want the fresh container mtime %v",
			items[0].PathModTime, containerModTime)
	}
	if !items[1].ModTime.Equal(containerModTime) {
		t.Errorf("newer container mtime was ignored: ModTime = %v; want %v",
			items[1].ModTime, containerModTime)
	}
	if !items[2].ModTime.Equal(containerModTime) || !items[2].PathModTime.IsZero() {
		t.Errorf("non-derived item = %v/%v; want the path's own mtime %v and no derived marker",
			items[2].ModTime, items[2].PathModTime, containerModTime)
	}
}

// TestCaptureCleanupTargetSnapshotRefusesLiveActivityDerivedTarget pins
// blocker 3: the mutation-boundary age recheck must read the activity signal,
// not just the container's own stat.
func TestCaptureCleanupTargetSnapshotRefusesLiveActivityDerivedTarget(t *testing.T) {
	home := t.TempDir()
	container := filepath.Join(home, ".gradle", "caches")
	if err := os.MkdirAll(container, 0o755); err != nil {
		t.Fatal(err)
	}
	containerModTime := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(container, containerModTime, containerModTime); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(container)
	if err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:        types.ToolBuildCache,
		Category:    types.CategoryBuildCache,
		Path:        container,
		CleanupKind: types.CleanupRemovePath,
		ModTime:     time.Now().Add(-5 * time.Minute),
		PathModTime: info.ModTime(),
	}

	if _, err := captureCleanupTargetSnapshot(item, types.PruneOptions{Age: 7 * 24 * time.Hour}); err == nil {
		t.Fatal("captureCleanupTargetSnapshot() error = nil; want a minimum-age refusal for live in-tree activity")
	}
	if _, err := captureCleanupTargetSnapshot(item, types.PruneOptions{Age: 7 * 24 * time.Hour, RelaxCacheAge: true}); err != nil {
		t.Fatalf("pressure-selected young cache must pass preflight: %v", err)
	}
	pinned := types.PruneOptions{
		Age:            7 * 24 * time.Hour,
		RelaxCacheAge:  true,
		PressureDevice: "other-volume",
	}
	if _, err := captureCleanupTargetSnapshot(item, pinned); err == nil {
		t.Fatal("off-volume cache must keep the age preflight when automatic pressure is pinned")
	}

	item.ModTime = time.Now().Add(-8 * 24 * time.Hour)
	if _, err := captureCleanupTargetSnapshot(item, types.PruneOptions{Age: 7 * 24 * time.Hour}); err != nil {
		t.Fatalf("captureCleanupTargetSnapshot() on an idle derived target = %v; want acceptance", err)
	}
}

// TestValidateRechecksNestedActivityAtMutationBarrier pins the window between
// preparation and mutation. Preparation happens before the confirmation prompt,
// so a cache that is idle when the snapshot is captured can go live while the
// user reads the plan. Only validate runs inside the pre-mutation barrier, so
// only validate can catch that.
func TestValidateRechecksNestedActivityAtMutationBarrier(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cache, nested := writeCacheActivityFixture(t, home, 30*24*time.Hour, 30*24*time.Hour)
	ctx := context.Background()
	minimumAge := 7 * 24 * time.Hour

	items, err := (&adapter.BuildCacheAdapter{}).Scan(ctx, types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	var item types.DebrisInfo
	for _, candidate := range items {
		if candidate.ID == "gradle" {
			item = candidate
			break
		}
	}
	if item.Path == "" {
		t.Fatalf("gradle cache not found in scan results: %+v", items)
	}
	if item.ModTime.IsZero() || !item.ModTime.Before(time.Now().Add(-minimumAge)) {
		t.Fatalf("scan-time ModTime = %v; want older than %s", item.ModTime, minimumAge)
	}
	if item.PathModTime.IsZero() || !item.PathModTime.Before(time.Now().Add(-minimumAge)) {
		t.Fatalf("scan-time PathModTime = %v; want older than %s", item.PathModTime, minimumAge)
	}

	// The whole tree is idle at preparation time, so the snapshot is captured.
	snapshot, err := captureCleanupTargetSnapshot(item, types.PruneOptions{Age: minimumAge})
	if err != nil {
		t.Fatalf("captureCleanupTargetSnapshot() on an idle cache = %v; want acceptance", err)
	}
	if err := snapshot.validate(ctx); err != nil {
		t.Fatalf("validate() on an idle cache = %v; want acceptance", err)
	}

	containerBefore, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(nested, now, now); err != nil {
		t.Fatal(err)
	}
	containerAfter, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if !containerAfter.ModTime().Equal(containerBefore.ModTime()) {
		t.Fatalf("nested touch changed container mtime from %v to %v", containerBefore.ModTime(), containerAfter.ModTime())
	}

	err = snapshot.validate(ctx)
	if err == nil || !strings.Contains(err.Error(), "younger than the configured minimum age") {
		t.Fatalf("validate() error = %v; want a minimum-age refusal from fresh nested activity", err)
	}
	if !errors.Is(err, errCleanupTargetYoungerThanMinimumAge) {
		t.Fatalf("validate() error %v does not match the minimum-age sentinel", err)
	}
}

func TestCleanJSONCLIContractCachedScanLiveNestedCacheRefusedByAgeGate(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	skipAgeGateIfVolumeRelaxesCaches(t, home)
	cache, _ := writeCacheActivityFixture(t, home, 30*24*time.Hour, 5*time.Minute)

	scanStdout, scanStderr, scanErr := runCleanJSONProcess(t, binary, home,
		"scan", "--json", "--root", home)
	if scanErr != nil {
		t.Fatalf("scan for cached-scan fixture failed: %v\nstdout=%s\nstderr=%s", scanErr, scanStdout, scanStderr)
	}
	if scanStderr != "" {
		t.Fatalf("scan for cached-scan fixture stderr = %q", scanStderr)
	}
	scanCachePaths := []string{
		filepath.Join(home, ".cache", "aibris", "last-scan.json"),
		filepath.Join(home, "Library", "Caches", "aibris", "last-scan.json"),
	}
	cacheWritten := false
	for _, scanCachePath := range scanCachePaths {
		if _, err := os.Stat(scanCachePath); err == nil {
			cacheWritten = true
			break
		}
	}
	if !cacheWritten {
		t.Fatalf("scan did not write last-scan cache in %v", scanCachePaths)
	}

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--category=build-cache", "--age=7d", "--root", home)
	if err != nil {
		t.Fatalf("cached nested cache clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("cached nested cache clean JSON stderr = %q", stderr)
	}
	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("cached nested cache JSON is invalid: %v\n%s", err, stdout)
	}
	if document.Evidence.Source != string(scanSourceCached) {
		t.Fatalf("clean evidence source = %q; want cached", document.Evidence.Source)
	}
	if document.Totals.Selected != 0 || document.Totals.Skipped != 1 {
		t.Fatalf("cached live nested cache was selected for removal: totals=%+v", document.Totals)
	}
	row := cacheActivityBuildCacheRow(t, document)
	if slices.Contains(row.ReasonCodes, "scan_evidence_unavailable") {
		t.Fatalf("cached live nested cache refused by an integrity error: %+v", row)
	}
	if !slices.Contains(row.ReasonCodes, "minimum_age") {
		t.Fatalf("cached live nested cache reason codes = %v; want minimum_age", row.ReasonCodes)
	}
	if _, err := os.Lstat(cache); err != nil {
		t.Fatalf("cached clean dry-run changed the live cache: %v", err)
	}
}

// TestCleanJSONCLIContractPostScanInTreeWriteIsRefusedAsMinimumAge is the
// end-to-end receipt shape for the same window: the cache is idle when it is
// scanned and selected, goes live before the barrier, and the refusal must
// reach automation as minimum_age rather than the generic execution_failed.
func TestCleanJSONCLIContractPostScanInTreeWriteIsRefusedAsMinimumAge(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	skipAgeGateIfVolumeRelaxesCaches(t, home)
	cache, nested := writeCacheActivityFixture(t, home, 30*24*time.Hour, 30*24*time.Hour)

	scanStdout, scanStderr, scanErr := runCleanJSONProcess(t, binary, home, "scan", "--json", "--root", home)
	if scanErr != nil {
		t.Fatalf("scan failed: %v\nstdout=%s\nstderr=%s", scanErr, scanStdout, scanStderr)
	}

	// The write lands after the scan and touches nothing the container's own
	// stat can see.
	containerBefore, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(nested, now, now); err != nil {
		t.Fatal(err)
	}
	containerAfter, err := os.Stat(cache)
	if err != nil {
		t.Fatal(err)
	}
	if !containerAfter.ModTime().Equal(containerBefore.ModTime()) {
		t.Fatalf("nested touch changed container mtime from %v to %v",
			containerBefore.ModTime(), containerAfter.ModTime())
	}

	stdout, _, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--json", "--force", "--category=build-cache", "--age=7d", "--root", home)
	if err == nil {
		t.Fatalf("clean succeeded on a cache that went live: %s", stdout)
	}
	var receipt cleanJSONReceipt
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("receipt is invalid JSON: %v\n%s", err, stdout)
	}
	if receipt.Status != cleanJSONReceiptFailed {
		t.Fatalf("receipt status = %q; want failed\n%s", receipt.Status, stdout)
	}
	if receipt.Totals.Requested != 1 || receipt.Totals.Failed != 1 {
		t.Fatalf("receipt totals = %+v; want one requested and one failed target", receipt.Totals)
	}
	if len(receipt.PhysicalTargets) != 1 {
		t.Fatalf("receipt physical_targets = %d; want one\n%s", len(receipt.PhysicalTargets), stdout)
	}
	reasonCodes := receipt.PhysicalTargets[0].ReasonCodes
	if !slices.Contains(reasonCodes, "minimum_age") {
		t.Fatalf("receipt reason codes = %v; want minimum_age", reasonCodes)
	}
	if slices.Contains(reasonCodes, "execution_failed") {
		t.Fatalf("receipt reason codes = %v; want the age refusal, not a generic failure", reasonCodes)
	}
	if _, statErr := os.Lstat(cache); statErr != nil {
		t.Fatalf("the live cache was removed anyway: %v", statErr)
	}
}

// TestCleanJSONCLIContractIdleNestedCacheIsStillCleaned is the regression
// blocker 1 introduced: a genuinely idle cache whose newest in-tree mtime
// simply differs from its container mtime used to be cleaned, and must be
// cleaned again.
func TestCleanJSONCLIContractIdleNestedCacheIsStillCleaned(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	cache, _ := writeCacheActivityFixture(t, home, 30*24*time.Hour, 8*24*time.Hour)

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--json", "--force", "--category=build-cache", "--age=7d", "--root", home)
	if err != nil {
		t.Fatalf("idle nested cache clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("idle nested cache clean JSON stderr = %q", stderr)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["status"] != cleanJSONReceiptSucceeded {
		t.Fatalf("idle nested cache receipt status = %v; want success\n%s", document["status"], stdout)
	}
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt(totals, "removed") != 1 {
		t.Fatalf("idle nested cache receipt totals = %+v; want one removed", totals)
	}
	if _, statErr := os.Lstat(cache); !os.IsNotExist(statErr) {
		t.Fatalf("idle nested cache survived cleanup: %v", statErr)
	}
}

// TestAgentStateGraceSurvivesInventoryRefresh pins the composition, not just
// the pieces: a store whose own directory mtime is long past the floor but
// whose session file was written minutes ago must still be held after the
// scan result passes through refreshCleanupInventoryMetadata. Round 1 of this
// change had the adapter right and lost the signal here, because the refresh
// overwrote ModTime with the directory's own mtime.
func TestAgentStateGraceSurvivesInventoryRefresh(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entry := writeAgentStateGraceFixture(t, home)
	session := filepath.Join(entry, "session.jsonl")

	stale := time.Now().Add(-30 * 24 * time.Hour)
	chtimesTree(t, entry, stale)
	written := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(session, written, written); err != nil {
		t.Fatal(err)
	}

	result, err := scanner.ScanWithOptions(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	refreshCleanupInventoryMetadata(result.Worktrees)

	opts := types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	var found bool
	for _, item := range result.Worktrees {
		if item.Path != entry {
			continue
		}
		found = true
		if item.Classification != types.EntryClassOrphaned {
			t.Fatalf("fixture store classification = %q; want orphaned", item.Classification)
		}
		eligible, reason := cleaner.EvaluateEligibility(item, opts, time.Now())
		if eligible || reason != cleaner.EligibilityReasonAgentStateMinIdleAge {
			t.Fatalf("refreshed store eligibility = %t/%q (ModTime %v); want held by the idle floor",
				eligible, reason, item.ModTime)
		}
	}
	if !found {
		t.Fatalf("scan did not report the fixture store: %+v", result.Worktrees)
	}
}

// TestAgentStateGraceSeesPostScanSessionAppend covers the cached-scan window:
// `clean` may run against a scan up to lastScanCacheMaxAge old, and an append
// to an existing session file moves neither the store directory's mtime nor
// anything else the refresh would otherwise observe. Agent-state gets no
// minimum age at the mutation barrier either, so the refresh is the last place
// this can be caught.
func TestAgentStateGraceSeesPostScanSessionAppend(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entry := writeAgentStateGraceFixture(t, home)
	session := filepath.Join(entry, "session.jsonl")

	stale := time.Now().Add(-30 * 24 * time.Hour)
	chtimesTree(t, entry, stale)

	result, err := scanner.ScanWithOptions(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Worktrees {
		if item.Path == entry && !item.ModTime.Equal(stale.Truncate(time.Second)) &&
			item.ModTime.After(time.Now().Add(-time.Hour)) {
			t.Fatalf("fixture was not idle at scan time: ModTime = %v", item.ModTime)
		}
	}

	// The session keeps writing after the scan; only the file's mtime moves.
	appended := time.Now()
	if err := os.Chtimes(session, appended, appended); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Lstat(entry)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.ModTime().After(time.Now().Add(-time.Hour)) {
		t.Fatalf("fixture invalid: the append moved the store directory mtime to %v", dirInfo.ModTime())
	}

	refreshCleanupInventoryMetadataWithContext(context.Background(), result.Worktrees)

	opts := types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	for _, item := range result.Worktrees {
		if item.Path != entry {
			continue
		}
		eligible, reason := cleaner.EvaluateEligibility(item, opts, time.Now())
		if eligible || reason != cleaner.EligibilityReasonAgentStateMinIdleAge {
			t.Fatalf("store written after the scan stayed eligible: %t/%q (ModTime %v)",
				eligible, reason, item.ModTime)
		}
		return
	}
	t.Fatalf("scan did not report the fixture store: %+v", result.Worktrees)
}
