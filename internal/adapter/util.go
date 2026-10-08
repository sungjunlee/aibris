package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

// EstimateDirSize measures a path with the same estimator scan uses, for
// callers that must re-derive a size after the scan (e.g. strip execution).
// It is report-only: unreadable entries are skipped while readable siblings
// still contribute bytes. Use EstimateDirSizeWithError to detect incompleteness.
func EstimateDirSize(ctx context.Context, path string) int64 {
	return estimateDirSize(ctx, path)
}

// EstimateDirSizeWithError retains partial byte observations but reports any
// missing evidence. Reporting callers may ignore the error; reclamation uses
// it to distinguish comparable approximate observations from a complete
// baseline followed by an incomplete residual walk.
func EstimateDirSizeWithError(ctx context.Context, path string) (int64, error) {
	activity := estimateDirActivityWithOptions(ctx, path, false)
	return activity.Size, activity.Err
}

// NewestTreeModTime reports the newest modification time observed anywhere in
// the tree at path, or the zero time when nothing readable was found. It is
// the same signal cache adapters record as ModTime, for callers that must
// re-derive it after the scan.
//
// This is a report-only lower bound when traversal is incomplete. Deletion
// approval must use CompleteTreeModTime instead.
func NewestTreeModTime(ctx context.Context, path string) time.Time {
	return estimateDirActivity(ctx, path).NewestModTime
}

// CompleteTreeModTime returns activity evidence only when every entry was
// observed. Any stat, traversal or cancellation error refuses the observation;
// callers must not approve deletion using the partial timestamp on error.
func CompleteTreeModTime(ctx context.Context, path string) (time.Time, error) {
	activity := estimateDirActivity(ctx, path)
	return activity.NewestModTime, activity.Err
}

func detectProjectName(path string) string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && !isHiddenDir(e.Name()) {
			return e.Name()
		}
	}
	return ""
}

// projectNameFromRecordedCWD labels a cwd-keyed store without requiring the
// recorded directory to still exist.
func projectNameFromRecordedCWD(path string) string {
	cleanPath := filepath.Clean(path)
	if cleanPath == string(filepath.Separator) || cleanPath == "." {
		return ""
	}
	return filepath.Base(cleanPath)
}

func isHiddenDir(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

func scanRootsOrHome(roots []string) ([]string, error) {
	if len(roots) > 0 {
		return roots, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []string{home}, nil
}

// IsWithin reports whether child is parent or nested under parent.
// Equality is within (filepath.Rel "." is true).
// On Windows, comparison is case-insensitive.
func IsWithin(parent, child string) bool {
	// Normalize paths for Windows case-insensitive comparison
	parentCmp := parent
	childCmp := child
	if runtime.GOOS == "windows" {
		parentCmp = strings.ToLower(filepath.Clean(parent))
		childCmp = strings.ToLower(filepath.Clean(child))
	}
	rel, err := filepath.Rel(parentCmp, childCmp)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func pathUnderRoots(path string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	cleanPath := canonicalExistingPath(path)
	for _, root := range roots {
		cleanRoot := canonicalExistingPath(root)
		if cleanPath == cleanRoot || IsWithin(cleanRoot, cleanPath) {
			return true
		}
	}
	return false
}

// applyCodexHomeScanRoots extends roots with uncovered Codex homes only for
// the default $HOME scan. Explicit --root is a hard boundary and is returned
// unchanged, including --root $HOME.
func applyCodexHomeScanRoots(opts types.ScanOptions, roots []string) ([]string, error) {
	if explicitScan(opts, roots) {
		return roots, nil
	}
	return appendUncoveredCodexHomes(roots)
}

func explicitScan(opts types.ScanOptions, roots []string) bool {
	if opts.ExplicitRoots {
		return true
	}
	return len(roots) > 0 && !isDefaultHomeScan(roots)
}

func isDefaultHomeScan(roots []string) bool {
	if len(roots) != 1 {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return canonicalExistingPath(roots[0]) == canonicalExistingPath(home)
}

// UncoveredCodexHomeWarning returns one path-free diagnostic when explicit
// scan roots do not cover a resolved Codex home. Default $HOME scans return
// no warning because those homes are still appended.
func UncoveredCodexHomeWarning(opts types.ScanOptions) (string, error) {
	if !explicitScan(opts, opts.Roots) {
		return "", nil
	}
	uncovered, err := uncoveredCodexHomes(opts.Roots)
	if err != nil || len(uncovered) == 0 {
		return "", err
	}
	return "configured Codex home is outside --root; not widening scan scope", nil
}

// appendUncoveredCodexHomes returns roots extended with every Codex home
// (primary, configured extras, and the verified Orca macOS home) not already under
// one of them. Scan roots default to $HOME while the Codex CLI honors
// CODEX_HOME, so a default scan must still cover an overridden home or its
// store would be silently filtered away.
func appendUncoveredCodexHomes(roots []string) ([]string, error) {
	uncovered, err := uncoveredCodexHomes(roots)
	if err != nil {
		return nil, err
	}
	return append(append([]string(nil), roots...), uncovered...), nil
}

func uncoveredCodexHomes(roots []string) ([]string, error) {
	homes, err := codexhome.Homes()
	if err != nil {
		return nil, err
	}
	var uncovered []string
	for _, home := range homes {
		if _, err := os.Stat(home); err != nil {
			continue
		}
		if !pathUnderRoots(home, roots) {
			uncovered = append(uncovered, home)
		}
	}
	return uncovered, nil
}
