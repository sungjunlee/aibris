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
		if item.Category != types.CategoryWorktree && !info.ModTime().Equal(item.ModTime) {
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
		info, identity, err := cleanupPathIdentity(item.Path)
		if err != nil || identity != expected.Identity || info.Mode().Type() != expected.Type {
			return false
		}
		items[i].ScanPathIdentity = expected.Identity
		items[i].ScanPathType = uint32(expected.Type)
	}
	return true
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
