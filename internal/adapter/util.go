package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

// EstimateDirSize measures a path with the same estimator scan uses, for
// callers that must re-derive a size after the scan (e.g. strip execution).
func EstimateDirSize(ctx context.Context, path string) int64 {
	return estimateDirSize(ctx, path)
}

// NewestTreeModTime reports the newest modification time observed anywhere in
// the tree at path, or the zero time when nothing readable was found. It is
// the same signal cache adapters record as ModTime, for callers that must
// re-derive it after the scan.
//
// The walk skips a subtree it cannot read, so an unreadable directory hides
// any newer mtime beneath it and the result can be older than the tree really
// is. Callers must therefore treat this as a lower bound and combine it with
// whatever activity they already recorded, never replace that record with it.
func NewestTreeModTime(ctx context.Context, path string) time.Time {
	return estimateDirActivity(ctx, path).NewestModTime
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
func IsWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func pathUnderRoots(path string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	cleanPath := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(cleanPath); err == nil {
		cleanPath = filepath.Clean(resolved)
	}
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if resolved, err := filepath.EvalSymlinks(cleanRoot); err == nil {
			cleanRoot = filepath.Clean(resolved)
		}
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
// scan roots do not cover a configured Codex home. Default $HOME scans return
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
// (CODEX_HOME plus any AIBRIS_CODEX_HOMES entries) that is not already under
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
