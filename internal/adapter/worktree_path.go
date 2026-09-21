package adapter

import (
	"path/filepath"
	"runtime"
	"strings"
)

// resolvedExistingPath is the cleaned path used for filesystem access.
// Symlinks are resolved when the path exists, and the filesystem spelling is
// preserved. It is not an identity key: on Windows two spellings of one path
// stay distinct here.
func resolvedExistingPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

// canonicalExistingPath is the identity key for worktree maps and equality.
// It uses the resolved path, then case-folds on Windows so a store and a
// lookup of the same path agree. Callers that open or report the path want
// resolvedExistingPath instead.
func canonicalExistingPath(path string) string {
	return canonicalPathKey(resolvedExistingPath(path))
}

func canonicalPathKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// sameCleanPath reports whether two cleaned paths are the same lexical path.
// It does not resolve symlinks, so a symlink and its target stay distinct
// when their spellings differ. Windows comparison ignores case only.
func sameCleanPath(left, right string) bool {
	return canonicalPathKey(filepath.Clean(left)) == canonicalPathKey(filepath.Clean(right))
}
