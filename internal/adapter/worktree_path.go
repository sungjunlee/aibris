package adapter

import (
	"path/filepath"
	"runtime"
	"strings"
)

// canonicalExistingPath returns a cleaned, normalized form of the path for
// identity and comparison purposes. On Windows, this includes lowercasing for
// case-insensitive matching. On Unix, the path is returned as-is after
// cleaning and symlink resolution.
func canonicalExistingPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if runtime.GOOS == "windows" {
		// Windows paths are case-insensitive; normalize to lowercase for comparison
		path = strings.ToLower(path)
	}
	return path
}
