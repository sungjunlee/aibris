package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestSaveLastScanCacheAtomicReplacement(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	seed := lastScanCache{
		SchemaVersion:    lastScanCacheSchemaVersion,
		ProviderIdentity: adapter.DefaultProviderIdentity(),
		CreatedAt:        time.Now(),
		Roots:            []string{home},
		Result:           types.ScanResult{TotalCount: 1},
	}
	if err := saveLastScanCache(seed); err != nil {
		t.Fatal(err)
	}
	path, err := lastScanCachePath()
	if err != nil {
		t.Fatal(err)
	}
	prev, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var worktrees []types.DebrisInfo
	for i := 0; i < 512; i++ {
		worktrees = append(worktrees, types.DebrisInfo{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       fmt.Sprintf("wt-%03d", i),
			Path:     filepath.Join(home, "worktrees", fmt.Sprintf("project-%03d", i)),
			Size:     int64(i+1) * 1024,
			Status:   types.WorktreeActive,
		})
	}
	replacement := lastScanCache{
		SchemaVersion:    lastScanCacheSchemaVersion,
		ProviderIdentity: adapter.DefaultProviderIdentity(),
		CreatedAt:        time.Now(),
		Roots:            []string{home},
		Result: types.ScanResult{
			Worktrees:  worktrees,
			TotalCount: len(worktrees),
		},
	}
	want, err := json.MarshalIndent(replacement, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(prev, want) {
		t.Fatal("fixture documents must differ")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var reads, torn, sawNew int64

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := saveLastScanCache(replacement); err != nil {
					t.Errorf("saveLastScanCache: %v", err)
					return
				}
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := os.ReadFile(path)
				if err != nil {
					atomic.AddInt64(&torn, 1)
					continue
				}
				atomic.AddInt64(&reads, 1)
				switch {
				case bytes.Equal(data, prev):
				case bytes.Equal(data, want):
					atomic.AddInt64(&sawNew, 1)
				default:
					atomic.AddInt64(&torn, 1)
				}
			}
		}()
	}

	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()

	if torn != 0 {
		t.Fatalf("readers observed %d partial or missing payloads; want 0", torn)
	}
	if reads == 0 {
		t.Fatal("readers observed nothing")
	}
	if sawNew == 0 {
		t.Fatal("readers never observed the replacement document; writers may not have run")
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(path) {
			t.Errorf("unexpected leftover file %q in cache directory", entry.Name())
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Errorf("cache permissions = %o; want 0644", perm)
	}
	if _, ok := readLastScanCache(); !ok {
		t.Fatal("final cache document must be readable")
	}
}

func TestReadLastScanCacheRejectsForeignProviderIdentity(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	foreign := adapter.Identity([]adapter.DebrisProvider{adapter.NewWorktreeAdapter()})
	if foreign == adapter.DefaultProviderIdentity() {
		t.Fatal("fixture requires a provider set whose identity differs from the default registry")
	}

	writeLastScanCache([]string{home}, foreign, &types.ScanResult{TotalCount: 1}, false)
	if _, _, ok := readFreshLastScanCache([]string{home}); ok {
		t.Fatal("readFreshLastScanCache accepted inventory produced by a foreign provider set; clean must fall back to a live scan")
	}

	writeLastScanCache([]string{home}, scanner.DefaultScanner.ProviderIdentity(), &types.ScanResult{TotalCount: 1}, false)
	if _, _, ok := readFreshLastScanCache([]string{home}); !ok {
		t.Fatal("readFreshLastScanCache rejected inventory produced by the default provider set")
	}
}

// TestWriteLastScanCacheKeepsCacheWithNewerInTreeActivity guards the scan
// side of the activity signal: a cache whose newest in-tree mtime differs from
// its container mtime must not read as "changed before cache write", which
// would silently invalidate the last-scan cache for everyone with an active
// build cache.
func TestWriteLastScanCacheKeepsCacheWithNewerInTreeActivity(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cache := filepath.Join(home, ".gradle", "caches")
	nested := filepath.Join(cache, "modules-2", "files-2.1")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(nested, "artifact.bin")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	chtimesTree(t, cache, time.Now().Add(-30*24*time.Hour))
	recent := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(artifact, recent, recent); err != nil {
		t.Fatal(err)
	}

	roots := []string{home}
	result, err := scanner.ScanWithOptions(context.Background(), types.ScanOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	var scanned *types.DebrisInfo
	for i := range result.Worktrees {
		if result.Worktrees[i].Path == cache {
			scanned = &result.Worktrees[i]
		}
	}
	if scanned == nil {
		t.Fatalf("scan did not report the fixture cache: %+v", result.Worktrees)
	}
	if !scanned.ModTime.After(scanned.PathModTime) {
		t.Fatalf("fixture must produce in-tree activity newer than the container: %+v", *scanned)
	}

	writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result, false)
	if _, ok := readLastScanCache(); !ok {
		t.Fatal("last-scan cache was invalidated for a home with active in-tree cache activity")
	}
	cached, _, ok := readFreshLastScanCache(roots)
	if !ok {
		t.Fatal("readFreshLastScanCache rejected a cache written for active in-tree cache activity")
	}
	for _, item := range cached.Worktrees {
		if item.Path != cache {
			continue
		}
		if !item.ModTime.Equal(scanned.ModTime) || !item.PathModTime.Equal(scanned.PathModTime) {
			t.Fatalf("cached activity signal = %v/%v; want %v/%v",
				item.ModTime, item.PathModTime, scanned.ModTime, scanned.PathModTime)
		}
		return
	}
	t.Fatalf("cached scan result lost the fixture cache: %+v", cached.Worktrees)
}

// A cache entry without a recorded path mtime reads as though ModTime were the
// path's own mtime, which turns the tree-activity signal off for the rest of
// the run. The whole cache must be refused rather than trusted in that weaker
// reading.
func TestReadFreshLastScanCacheRejectsCacheEntryWithoutPathModTime(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cache := filepath.Join(home, ".gradle", "caches")
	nested := filepath.Join(cache, "modules-2", "files-2.1")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(nested, "artifact.bin")
	if err := os.WriteFile(artifact, []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	chtimesTree(t, cache, time.Now().Add(-30*24*time.Hour))

	roots := []string{home}
	result, err := scanner.ScanWithOptions(context.Background(), types.ScanOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result, false)
	if _, _, ok := readFreshLastScanCache(roots); !ok {
		t.Fatal("readFreshLastScanCache rejected an intact cache; fixture is wrong")
	}

	stored, ok := readLastScanCache()
	if !ok {
		t.Fatal("last-scan cache was not written")
	}
	stripped := false
	for i := range stored.Result.Worktrees {
		if stored.Result.Worktrees[i].Path != cache {
			continue
		}
		stored.Result.Worktrees[i].PathModTime = time.Time{}
		stripped = true
	}
	if !stripped {
		t.Fatalf("cached scan result lost the fixture cache: %+v", stored.Result.Worktrees)
	}
	if err := saveLastScanCache(stored); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := readFreshLastScanCache(roots); ok {
		t.Fatal("readFreshLastScanCache accepted a cache entry with no recorded path mtime")
	}
}

// Agent-state stores derive ModTime from in-tree activity too, so the same
// refusal has to cover them: without PathModTime the store's ModTime would be
// read as the directory's own mtime and the --agent-state-grace floor would
// silently go back to measuring the wrong thing.
func TestReadFreshLastScanCacheRejectsAgentStateEntryWithoutPathModTime(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entry := filepath.Join(home, ".claude", "projects", "orphaned-entry")
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(entry, "session.jsonl")
	absentCWD := filepath.Join(home, "missing", "project")
	if err := os.WriteFile(session, []byte(fmt.Sprintf("{\"cwd\":%q}\n", absentCWD)), 0o644); err != nil {
		t.Fatal(err)
	}
	chtimesTree(t, entry, time.Now().Add(-48*time.Hour))

	roots := []string{home}
	result, err := scanner.ScanWithOptions(context.Background(), types.ScanOptions{Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result, false)
	if _, _, ok := readFreshLastScanCache(roots); !ok {
		t.Fatal("readFreshLastScanCache rejected an intact cache; fixture is wrong")
	}

	stored, ok := readLastScanCache()
	if !ok {
		t.Fatal("last-scan cache was not written")
	}
	stripped := false
	for i := range stored.Result.Worktrees {
		if stored.Result.Worktrees[i].Path != entry {
			continue
		}
		if stored.Result.Worktrees[i].Category != types.CategoryAgentState {
			t.Fatalf("fixture entry is %q; want agent-state", stored.Result.Worktrees[i].Category)
		}
		stored.Result.Worktrees[i].PathModTime = time.Time{}
		stripped = true
	}
	if !stripped {
		t.Fatalf("cached scan result lost the fixture store: %+v", stored.Result.Worktrees)
	}
	if err := saveLastScanCache(stored); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := readFreshLastScanCache(roots); ok {
		t.Fatal("readFreshLastScanCache accepted an agent-state entry with no recorded path mtime")
	}
}

func TestReadLastScanCacheRejectsMalformedPayload(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path, err := lastScanCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version": 5, "provider_identity": "x",`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readLastScanCache(); ok {
		t.Fatal("readLastScanCache accepted a torn JSON payload")
	}
	if _, _, ok := readFreshLastScanCache([]string{home}); ok {
		t.Fatal("readFreshLastScanCache accepted a torn JSON payload; clean must fall back to a live scan")
	}
	if err := saveLastScanCache(lastScanCache{
		SchemaVersion:    lastScanCacheSchemaVersion,
		ProviderIdentity: adapter.DefaultProviderIdentity(),
		CreatedAt:        time.Now(),
		Roots:            []string{home},
		Result:           types.ScanResult{TotalCount: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := readLastScanCache(); !ok {
		t.Fatal("valid cache must be readable after malformed payload is replaced")
	}
}
