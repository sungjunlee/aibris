package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type lastScanTargetEvidence struct {
	Identity string      `json:"identity"`
	Type     os.FileMode `json:"type"`
}

func captureLastScanTargetEvidence(items []types.DebrisInfo) (map[string]lastScanTargetEvidence, error) {
	evidence := make(map[string]lastScanTargetEvidence, len(items))
	var errs []error
	for i, item := range items {
		items[i].ScanPathEvidenceRequired = true
		items[i].ScanPathIdentity = ""
		items[i].ScanPathType = 0
		info, identity, err := cleanupPathIdentity(item.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("capturing scan target evidence for %q: %w", item.Path, err))
			continue
		}
		// This is a tamper check on the path itself, so it must compare against
		// the path's own recorded mtime. ModTime can be derived from activity
		// anywhere in the tree, and comparing that to a fresh stat would report
		// every actively written cache as changed since the scan.
		recorded := item.ModTime
		if !item.PathModTime.IsZero() {
			recorded = item.PathModTime
		}
		if item.Category != types.CategoryWorktree && !info.ModTime().Equal(recorded) {
			errs = append(errs, fmt.Errorf("scan target changed before cache write: %q", item.Path))
			continue
		}
		evidence[filepath.Clean(item.Path)] = lastScanTargetEvidence{
			Identity: identity,
			Type:     info.Mode().Type(),
		}
		items[i].ScanPathIdentity = identity
		items[i].ScanPathType = uint32(info.Mode().Type())
	}
	return evidence, errors.Join(errs...)
}

func validateLastScanTargetEvidence(items []types.DebrisInfo, evidence map[string]lastScanTargetEvidence) bool {
	paths := make(map[string]struct{}, len(items))
	for i, item := range items {
		items[i].ScanPathEvidenceRequired = true
		paths[filepath.Clean(item.Path)] = struct{}{}
	}
	if len(paths) != len(evidence) {
		return false
	}
	for i, item := range items {
		expected, ok := evidence[filepath.Clean(item.Path)]
		if !ok {
			return false
		}
		// A cached cache-category item without PathModTime would be treated as
		// though its ModTime were the path's own mtime, which silently turns
		// off the tree-activity signal for the rest of the run. Refuse the
		// whole cache and rescan instead of trusting the weaker reading.
		if cacheActivityCategory(item.Category) && item.PathModTime.IsZero() {
			return false
		}
		info, identity, err := cleanupPathIdentity(item.Path)
		if err != nil || identity != expected.Identity || info.Mode().Type() != expected.Type {
			return false
		}
		items[i].ScanPathIdentity = expected.Identity
		items[i].ScanPathType = uint32(expected.Type)
	}
	return true
}

// cacheActivityCategory reports whether a category's adapters derive ModTime
// from activity inside the tree and therefore must record PathModTime.
func cacheActivityCategory(category types.Category) bool {
	return category == types.CategoryBuildCache || category == types.CategoryOtherCache
}

func cleanupPathIdentity(path string) (os.FileInfo, string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("symbolic-link cleanup targets are not cacheable")
	}
	identity, err := platformCleanupPathIdentity(path)
	if err != nil {
		return nil, "", err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !os.SameFile(before, after) || before.Mode().Type() != after.Mode().Type() {
		return nil, "", fmt.Errorf("path changed while capturing identity")
	}
	return after, identity, nil
}
