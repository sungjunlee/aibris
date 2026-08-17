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

	lastScanSelectorDelete   = "delete"
	lastScanSelectorStrip    = "strip"
	lastScanSelectorPressure = "pressure"

	lastScanRescanProvider   = "scanning again: provider set changed"
	lastScanRescanActivity   = "scanning again: activity evidence missing"
	lastScanRescanEvidence   = "scanning again: target evidence failed"
	lastScanRescanStale      = "scanning again: last scan is stale"
	lastScanRescanSelector   = "scanning again: selector does not match"
	lastScanRescanExclusions = "scanning again: exclusions differ"
	lastScanRescanIncomplete = "scanning again: last scan is incomplete"
	lastScanRescanSchema     = "scanning again: cache schema differs"
	lastScanRescanRoots      = "scanning again: scan roots differ"
)

type lastScanCache struct {
	SchemaVersion             int                               `json:"schema_version"`
	ProviderIdentity          string                            `json:"provider_identity"`
	RetentionProviderIdentity string                            `json:"retention_provider_identity"`
	Selector                  string                            `json:"selector,omitempty"`
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
		Selector:                  lastScanSelectorIdentity(),
		CreatedAt:                 time.Now(),
		Roots:                     append([]string(nil), roots...),
		Result:                    *result,
		TargetEvidence:            evidence,
	})
}

func lastScanSelectorIdentity() string {
	if cleanStrip {
		return lastScanSelectorStrip
	}
	if cleanPressure {
		return lastScanSelectorPressure
	}
	return lastScanSelectorDelete
}

func lastScanSelectorValue(selector string) string {
	if selector == "" {
		return lastScanSelectorDelete
	}
	return selector
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
	result, age, reason := evaluateLastScanCache(roots)
	return result, age, reason == "" && result != nil
}

// lastScanCacheReuseDecision reports a reusable last-scan inventory or a
// one-line reason to scan again. An empty reason with a nil result means
// there is no cache to reuse or explain.
func lastScanCacheReuseDecision(roots, excludes []string) (*types.ScanResult, time.Duration, string) {
	if len(excludes) > 0 {
		if _, ok := readLastScanCache(); ok {
			return nil, 0, lastScanRescanExclusions
		}
		return nil, 0, ""
	}
	result, age, reason := evaluateLastScanCache(roots)
	if result != nil && scanResultHasExclusions(result) {
		return nil, age, lastScanRescanExclusions
	}
	return result, age, reason
}

func evaluateLastScanCache(roots []string) (*types.ScanResult, time.Duration, string) {
	cache, ok := readLastScanCache()
	if !ok {
		return nil, 0, ""
	}
	age := time.Since(cache.CreatedAt)
	if cache.SchemaVersion != lastScanCacheSchemaVersion {
		return nil, age, lastScanRescanSchema
	}
	if age < 0 || age > lastScanCacheMaxAge {
		return nil, age, lastScanRescanStale
	}
	if cache.ProviderIdentity == "" || cache.ProviderIdentity != adapter.DefaultProviderIdentity() {
		return nil, age, lastScanRescanProvider
	}
	if cache.RetentionProviderIdentity == "" ||
		cache.RetentionProviderIdentity != retention.DefaultProviderIdentity() {
		return nil, age, lastScanRescanProvider
	}
	if !slices.Equal(cache.Roots, roots) {
		return nil, age, lastScanRescanRoots
	}
	if lastScanSelectorValue(cache.Selector) != lastScanSelectorIdentity() {
		return nil, age, lastScanRescanSelector
	}
	if cache.Result.Partial() {
		return nil, age, lastScanRescanIncomplete
	}
	for _, item := range cache.Result.Worktrees {
		if treeActivityCategory(item.Category) && item.PathModTime.IsZero() {
			return nil, age, lastScanRescanActivity
		}
	}
	if !validateLastScanTargetEvidence(cache.Result.Worktrees, cache.TargetEvidence) {
		return nil, age, lastScanRescanEvidence
	}
	return &cache.Result, age, ""
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
