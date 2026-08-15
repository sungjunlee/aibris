package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

// jsProjectManifestMarkers identify a JS/TS checkout at the fixed
// checkout-root position. Only these exact paths gate the node_modules strip
// candidate; there is no recursive discovery of manifests or subtrees.
var jsProjectManifestMarkers = []string{
	"package.json",
	"yarn.lock",
	"pnpm-lock.yaml",
	"package-lock.json",
	"bun.lockb",
}

// worktreeStripCandidates returns regenerable subtree candidates at fixed
// known-relative positions inside the checkout rooted at checkoutPath, gated
// by detected project-type markers. Candidates may not exist; callers filter.
func worktreeStripCandidates(checkoutPath string) []string {
	var candidates []string
	for _, marker := range jsProjectManifestMarkers {
		if stripMarkerFileExists(filepath.Join(checkoutPath, marker)) {
			candidates = append(candidates, filepath.Join(checkoutPath, "node_modules"))
			break
		}
	}
	if stripMarkerDirExists(filepath.Join(checkoutPath, "android")) {
		candidates = append(candidates,
			filepath.Join(checkoutPath, "android", "build"),
			filepath.Join(checkoutPath, "android", ".gradle"),
			filepath.Join(checkoutPath, "android", "app", "build"),
		)
	}
	if stripMarkerDirExists(filepath.Join(checkoutPath, "ios")) {
		candidates = append(candidates,
			filepath.Join(checkoutPath, "ios", "Pods"),
			filepath.Join(checkoutPath, "ios", "build"),
		)
	}
	return candidates
}

// strippableSubtrees inventories the existing regenerable subtrees inside an
// active worktree checkout and measures them with the same estimator scan
// uses for unit sizes. Orphaned and plain-dir units carry no strippable
// info: their Git evidence is unavailable or absent, so strip safety can
// never be proved for them.
func (a *WorktreeAdapter) strippableSubtrees(ctx context.Context, checkoutPath string, status types.WorktreeStatus) (int64, []string) {
	if status != types.WorktreeActive {
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, nil
	}
	var paths []string
	for _, candidate := range worktreeStripCandidates(checkoutPath) {
		if stripMarkerDirExists(candidate) {
			paths = append(paths, candidate)
		}
	}
	if len(paths) == 0 {
		return 0, nil
	}
	sizes := estimateDirSizes(ctx, paths)
	var total int64
	for _, path := range paths {
		total += sizes[path]
	}
	return total, paths
}

func stripMarkerDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func stripMarkerFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
