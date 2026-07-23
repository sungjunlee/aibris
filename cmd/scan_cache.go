package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

const (
	lastScanCacheSchemaVersion = 1
	lastScanCacheMaxAge        = 5 * time.Minute
)

type lastScanCache struct {
	SchemaVersion int              `json:"schema_version"`
	CreatedAt     time.Time        `json:"created_at"`
	Roots         []string         `json:"roots"`
	Result        types.ScanResult `json:"result"`
}

func writeLastScanCache(roots []string, result *types.ScanResult) {
	if result == nil {
		return
	}
	if result.Partial() {
		invalidateLastScanCache()
		return
	}
	_ = saveLastScanCache(lastScanCache{
		SchemaVersion: lastScanCacheSchemaVersion,
		CreatedAt:     time.Now(),
		Roots:         append([]string(nil), roots...),
		Result:        *result,
	})
}

func invalidateLastScanCache() {
	path, err := lastScanCachePath()
	if err == nil {
		_ = os.Remove(path)
	}
}

func saveLastScanCache(cache lastScanCache) error {
	path, err := lastScanCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func readFreshLastScanCache(roots []string) (*types.ScanResult, time.Duration, bool) {
	cache, ok := readLastScanCache()
	if !ok {
		return nil, 0, false
	}
	age := time.Since(cache.CreatedAt)
	if cache.SchemaVersion != lastScanCacheSchemaVersion || age < 0 || age > lastScanCacheMaxAge {
		return nil, age, false
	}
	if !slices.Equal(cache.Roots, roots) {
		return nil, age, false
	}
	if cache.Result.Partial() {
		return nil, age, false
	}
	return &cache.Result, age, true
}

func readLastScanCache() (lastScanCache, bool) {
	path, err := lastScanCachePath()
	if err != nil {
		return lastScanCache{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return lastScanCache{}, false
	}
	var cache lastScanCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return lastScanCache{}, false
	}
	return cache, true
}

func lastScanCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aibris", "last-scan.json"), nil
}
