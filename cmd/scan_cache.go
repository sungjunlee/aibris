package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	// Bump this explicit compatibility revision when cache format or provider
	// behavior changes without a concrete provider-membership change.
	lastScanCacheSchemaVersion = 7
	lastScanCacheMaxAge        = 5 * time.Minute
)

type lastScanCache struct {
	SchemaVersion             int                               `json:"schema_version"`
	ProviderIdentity          string                            `json:"provider_identity"`
	RetentionProviderIdentity string                            `json:"retention_provider_identity"`
	CreatedAt                 time.Time                         `json:"created_at"`
	Roots                     []string                          `json:"roots"`
	Result                    types.ScanResult                  `json:"result"`
	TargetEvidence            map[string]lastScanTargetEvidence `json:"target_evidence,omitempty"`
}

func writeLastScanCache(roots []string, identity string, result *types.ScanResult) {
	if result == nil {
		return
	}
	if result.Partial() {
		invalidateLastScanCache()
		return
	}
	evidence, err := captureLastScanTargetEvidence(result.Worktrees)
	if err != nil {
		invalidateLastScanCache()
		return
	}
	_ = saveLastScanCache(lastScanCache{
		SchemaVersion:             lastScanCacheSchemaVersion,
		ProviderIdentity:          identity,
		RetentionProviderIdentity: retention.DefaultProviderIdentity(),
		CreatedAt:                 time.Now(),
		Roots:                     append([]string(nil), roots...),
		Result:                    *result,
		TargetEvidence:            evidence,
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
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	// Write to a uniquely named sibling then rename over last-scan.json so
	// concurrent readers never observe a torn document.
	tmp, err := os.CreateTemp(dir, "last-scan-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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
	if cache.ProviderIdentity == "" || cache.ProviderIdentity != adapter.DefaultProviderIdentity() {
		return nil, age, false
	}
	if cache.RetentionProviderIdentity == "" ||
		cache.RetentionProviderIdentity != retention.DefaultProviderIdentity() {
		return nil, age, false
	}
	if !slices.Equal(cache.Roots, roots) {
		return nil, age, false
	}
	if cache.Result.Partial() {
		return nil, age, false
	}
	if !validateLastScanTargetEvidence(cache.Result.Worktrees, cache.TargetEvidence) {
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
