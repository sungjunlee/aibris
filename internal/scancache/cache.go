package scancache

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
	lastScanCacheSchemaVersion = 10
	lastScanCacheMaxAge        = 5 * time.Minute

	SchemaVersion = lastScanCacheSchemaVersion
	MaxAge        = lastScanCacheMaxAge
)

// Document is the on-disk last-scan snapshot shared by `aibris scan` writes
// and `aibris clean` reuse.
type Document struct {
	SchemaVersion             int                       `json:"schema_version"`
	ProviderIdentity          string                    `json:"provider_identity"`
	RetentionProviderIdentity string                    `json:"retention_provider_identity"`
	Selector                  string                    `json:"selector,omitempty"`
	CreatedAt                 time.Time                 `json:"created_at"`
	Roots                     []string                  `json:"roots"`
	ExplicitRoots             bool                      `json:"explicit_roots,omitempty"`
	Result                    types.ScanResult          `json:"result"`
	TargetEvidence            map[string]TargetEvidence `json:"target_evidence,omitempty"`
}

type lastScanCache = Document

// lastScanCacheIdentity is the reuse key shared by `aibris scan` writes and
// `aibris clean` reads. Roots, provider membership, retention membership, and
// the explicit-root flag are the identity. Schema freshness is separate; there
// is no second fingerprint or on-disk cache format.
type lastScanCacheIdentity struct {
	providerIdentity          string
	retentionProviderIdentity string
	roots                     []string
	explicitRoots             bool
}

func lastScanCacheIdentityOf(roots []string, providerIdentity string, explicit bool) lastScanCacheIdentity {
	return lastScanCacheIdentity{
		providerIdentity:          providerIdentity,
		retentionProviderIdentity: retention.DefaultProviderIdentity(),
		roots:                     append([]string(nil), roots...),
		explicitRoots:             explicit,
	}
}

func currentLastScanCacheIdentity(roots []string, explicit bool) lastScanCacheIdentity {
	return lastScanCacheIdentityOf(roots, adapter.DefaultProviderIdentity(), explicit)
}

func (id lastScanCacheIdentity) mismatchReason(cache lastScanCache) string {
	if cache.ProviderIdentity == "" || cache.ProviderIdentity != id.providerIdentity {
		return "provider set changed"
	}
	if cache.RetentionProviderIdentity == "" ||
		cache.RetentionProviderIdentity != id.retentionProviderIdentity {
		return "provider set changed"
	}
	if !slices.Equal(cache.Roots, id.roots) || cache.ExplicitRoots != id.explicitRoots {
		return "scan roots changed"
	}
	return ""
}

func Write(roots []string, identity string, result *types.ScanResult, explicit bool) {
	writeLastScanCache(roots, identity, result, explicit)
}

func WriteForSelector(roots []string, identity, selector string, explicit bool, result *types.ScanResult) {
	writeLastScanCacheForSelector(roots, identity, selector, explicit, result)
}

func writeLastScanCache(roots []string, identity string, result *types.ScanResult, explicit bool) {
	writeLastScanCacheForSelector(roots, identity, "", explicit, result)
}

func writeLastScanCacheForSelector(roots []string, identity, selector string, explicit bool, result *types.ScanResult) {
	if result == nil {
		return
	}
	evidence, ok := lastScanWriteEvidence(result)
	if !ok {
		return
	}
	id := lastScanCacheIdentityOf(roots, identity, explicit)
	_ = saveLastScanCache(lastScanCache{
		SchemaVersion:             lastScanCacheSchemaVersion,
		ProviderIdentity:          id.providerIdentity,
		RetentionProviderIdentity: id.retentionProviderIdentity,
		Selector:                  selector,
		CreatedAt:                 time.Now(),
		Roots:                     id.roots,
		ExplicitRoots:             id.explicitRoots,
		Result:                    *result,
		TargetEvidence:            evidence,
	})
}

func lastScanWriteEvidence(result *types.ScanResult) (map[string]lastScanTargetEvidence, bool) {
	if result.Partial() {
		invalidateLastScanCache()
		return nil, false
	}
	evidence, err := captureLastScanTargetEvidence(result.Worktrees)
	if err != nil {
		invalidateLastScanCache()
		return nil, false
	}
	return evidence, true
}

func Invalidate() {
	invalidateLastScanCache()
}

func invalidateLastScanCache() {
	path, err := lastScanCachePath()
	if err == nil {
		_ = os.Remove(path)
	}
}

func Save(cache Document) error {
	return saveLastScanCache(cache)
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

func ReadFresh(roots []string) (*types.ScanResult, time.Duration, bool) {
	return readFreshLastScanCache(roots)
}

func readFreshLastScanCache(roots []string) (*types.ScanResult, time.Duration, bool) {
	result, age, _, ok := inspectLastScanCache(roots, "", false)
	return result, age, ok
}

func Inspect(roots []string, selector string, explicit bool) (*types.ScanResult, time.Duration, string, bool) {
	return inspectLastScanCache(roots, selector, explicit)
}

func inspectLastScanCache(roots []string, selector string, explicit bool) (*types.ScanResult, time.Duration, string, bool) {
	cache, ok := readLastScanCache()
	if !ok {
		return nil, 0, "", false
	}
	age := time.Since(cache.CreatedAt)
	if reason := lastScanReuseRefuseReason(cache, roots, selector, explicit, age); reason != "" {
		return nil, age, reason, false
	}
	result := cache.Result
	return &result, age, "", true
}

func lastScanReuseRefuseReason(cache lastScanCache, roots []string, selector string, explicit bool, age time.Duration) string {
	if reason := lastScanFreshnessReason(cache, age); reason != "" {
		return reason
	}
	if reason := lastScanIdentityReason(cache, roots, explicit); reason != "" {
		return reason
	}
	if !validateLastScanTargetEvidence(cache.Result.Worktrees, cache.TargetEvidence) {
		return "activity evidence missing"
	}
	return lastScanSelectorReason(cache.Selector, selector)
}

func lastScanFreshnessReason(cache lastScanCache, age time.Duration) string {
	if cache.SchemaVersion != lastScanCacheSchemaVersion || age < 0 || age > lastScanCacheMaxAge {
		return "cache stale"
	}
	if cache.Result.Partial() {
		return "cache stale"
	}
	return ""
}

func lastScanIdentityReason(cache lastScanCache, roots []string, explicit bool) string {
	return currentLastScanCacheIdentity(roots, explicit).mismatchReason(cache)
}

func IdentityMismatch(roots []string, explicit bool, cache Document) string {
	return currentLastScanCacheIdentity(roots, explicit).mismatchReason(cache)
}

func lastScanSelectorReason(cached, want string) string {
	if cached != "" && want != "" && cached != want {
		return "different cleanup selectors"
	}
	return ""
}

func ClaimSelector(selector string) bool {
	return claimLastScanSelector(selector)
}

func claimLastScanSelector(selector string) bool {
	if selector == "" {
		return true
	}
	cache, ok := readLastScanCache()
	if !ok || cache.Selector == selector {
		return ok
	}
	if cache.Selector != "" {
		return false
	}
	return persistLastScanSelector(cache, selector)
}

func persistLastScanSelector(cache lastScanCache, selector string) bool {
	cache.Selector = selector
	if err := saveLastScanCache(cache); err != nil {
		return false
	}
	return lastScanSelectorHeld(selector)
}

func lastScanSelectorHeld(selector string) bool {
	cache, ok := readLastScanCache()
	return ok && cache.Selector == selector
}

func Read() (Document, bool) {
	return readLastScanCache()
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

func Path() (string, error) {
	return lastScanCachePath()
}

func lastScanCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aibris", "last-scan.json"), nil
}
