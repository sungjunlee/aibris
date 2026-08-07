package cmd

import (
	"bytes"
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
	"github.com/sungjunlee/aibris/internal/types"
)

func TestSaveLastScanCacheAtomicReplacement(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

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
	t.Setenv("HOME", home)

	foreign := adapter.Identity([]adapter.DebrisProvider{adapter.NewWorktreeAdapter()})
	if foreign == adapter.DefaultProviderIdentity() {
		t.Fatal("fixture requires a provider set whose identity differs from the default registry")
	}

	writeLastScanCache([]string{home}, foreign, &types.ScanResult{TotalCount: 1})
	if _, _, ok := readFreshLastScanCache([]string{home}); ok {
		t.Fatal("readFreshLastScanCache accepted inventory produced by a foreign provider set; clean must fall back to a live scan")
	}

	writeLastScanCache([]string{home}, scanner.DefaultScanner.ProviderIdentity(), &types.ScanResult{TotalCount: 1})
	if _, _, ok := readFreshLastScanCache([]string{home}); !ok {
		t.Fatal("readFreshLastScanCache rejected inventory produced by the default provider set")
	}
}

func TestReadLastScanCacheRejectsMalformedPayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
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
