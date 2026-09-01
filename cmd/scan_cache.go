package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	// Bump this explicit compatibility revision when cache format or provider
	// behavior changes without a concrete provider-membership change.
	lastScanCacheSchemaVersion = 10
	lastScanCacheMaxAge        = 5 * time.Minute
)

type lastScanCache struct {
	SchemaVersion             int                               `json:"schema_version"`
	ProviderIdentity          string                            `json:"provider_identity"`
	RetentionProviderIdentity string                            `json:"retention_provider_identity"`
	Selector                  string                            `json:"selector,omitempty"`
	CreatedAt                 time.Time                         `json:"created_at"`
	Roots                     []string                          `json:"roots"`
	ExplicitRoots             bool                              `json:"explicit_roots,omitempty"`
	Result                    types.ScanResult                  `json:"result"`
	TargetEvidence            map[string]lastScanTargetEvidence `json:"target_evidence,omitempty"`
}

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
	result, age, _, ok := inspectLastScanCache(roots, "", false)
	return result, age, ok
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

func emitCachedExplicitRootWarning(roots []string, explicit bool) {
	warning, err := adapter.UncoveredCodexHomeWarning(types.ScanOptions{
		Roots:         roots,
		ExplicitRoots: explicit,
	})
	if err != nil || warning == "" {
		return
	}
	fmt.Fprint(os.Stderr, "warning: "+warning+"\n")
}

func lastScanSelectorReason(cached, want string) string {
	if cached != "" && want != "" && cached != want {
		return "different cleanup selectors"
	}
	return ""
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

// lastScanSession is the shared cache session used by scan (write) and clean
// (reuse or live scan). Clean's scan-for-clean path loads through this type
// rather than a private fingerprint or schema.
type lastScanSession struct {
	roots    []string
	excludes []string
	selector string
	explicit bool
	progress bool
}

func loadLastScanSession(ctx context.Context, roots, excludes []string, selector string, explicit, progress bool) (*types.ScanResult, scanSource, error) {
	return lastScanSession{
		roots:    roots,
		excludes: excludes,
		selector: selector,
		explicit: explicit,
		progress: progress,
	}.load(ctx)
}

func (s lastScanSession) load(ctx context.Context) (*types.ScanResult, scanSource, error) {
	if result, source, ok := s.tryCached(); ok {
		if err := requireCompleteScan(result); err != nil {
			return nil, scanSource{}, err
		}
		return result, source, nil
	}
	return s.liveScan(ctx)
}

func (s lastScanSession) tryCached() (*types.ScanResult, scanSource, bool) {
	if reason, skip := s.excludedReason(); skip {
		printLastScanRescan(reason, s.progress)
		return nil, scanSource{}, false
	}
	readAt := time.Now()
	result, age, reason, ok := inspectLastScanCache(s.roots, s.selector, s.explicit)
	if ok && scanResultHasExclusions(result) {
		ok = false
		reason = "cached scan used exclusions"
	}
	if !ok || !claimLastScanSelector(s.selector) {
		printLastScanRescan(cachedScanMissReason(ok, reason), s.progress)
		return nil, scanSource{}, false
	}
	emitCachedExplicitRootWarning(s.roots, s.explicit)
	printLastScanReuse(age, s.progress)
	return result, scanSource{
		Kind:       scanSourceCached,
		Age:        age,
		ObservedAt: readAt.Add(-age),
	}, true
}

func (s lastScanSession) excludedReason() (string, bool) {
	if len(s.excludes) == 0 {
		return "", false
	}
	_, _, reason, ok := inspectLastScanCache(s.roots, s.selector, s.explicit)
	if !ok && reason == "" {
		return "", true
	}
	return "cleanup exclusions requested", true
}

func cachedScanMissReason(ok bool, reason string) string {
	if !ok {
		return reason
	}
	return "cache selector unavailable"
}

func printLastScanReuse(age time.Duration, show bool) {
	if !show {
		return
	}
	fmt.Printf("using last scan from %s ago\n", shortDurationString(age))
}

func printLastScanRescan(reason string, show bool) {
	if !show || reason == "" {
		return
	}
	fmt.Printf("scanning again: %s\n", reason)
}

func (s lastScanSession) liveScan(ctx context.Context) (*types.ScanResult, scanSource, error) {
	result, err := s.runLive(ctx)
	if err != nil {
		return nil, scanSource{}, err
	}
	if err := requireCompleteScan(result); err != nil {
		return nil, scanSource{}, err
	}
	writeLastScanCacheForSelector(s.roots, scanner.DefaultScanner.ProviderIdentity(), s.selector, s.explicit, result)
	return result, scanSource{Kind: scanSourceLive}, nil
}

func (s lastScanSession) runLive(ctx context.Context) (*types.ScanResult, error) {
	if s.progress {
		progress := newScanProgressPrinter(os.Stdout)
		result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{
			Roots:         s.roots,
			ExplicitRoots: s.explicit,
			Excludes:      s.excludes,
			OnProgress:    progress.Handle,
		})
		progress.Stop()
		return result, err
	}
	quietScanner := scanner.NewWithRetentionProviders(
		scanner.DefaultScanner.Providers,
		scanner.DefaultScanner.RetentionProviders,
	)
	quietScanner.ErrorWriter = io.Discard
	return quietScanner.ScanWithOptions(ctx, types.ScanOptions{
		Roots:         s.roots,
		ExplicitRoots: s.explicit,
		Excludes:      s.excludes,
	})
}

// scanResultHasExclusions reports whether a cached scan was produced with
// user exclusions applied, so a plain clean never reuses a filtered cache.
func scanResultHasExclusions(result *types.ScanResult) bool {
	return result.ExcludedByUser > 0 || len(result.ExcludedScopes) > 0 || len(result.RejectedExcludes) > 0
}
