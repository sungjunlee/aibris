package adapter

import (
	"fmt"
	"os"
	"path/filepath"
)

// RefuseStaleGoCache reports an error when the live GOCACHE path no longer
// matches the path recorded at scan time. `go clean -cache` would otherwise
// mutate a different directory than the planned item.
func RefuseStaleGoCache(planned string) error {
	live, ok := effectiveGoCache()
	if !ok {
		return fmt.Errorf("live GOCACHE could not be resolved")
	}
	if !sameCachePath(live, planned) {
		return fmt.Errorf("live GOCACHE %q no longer matches planned %q", live, planned)
	}
	return nil
}

func effectiveGoCache() (string, bool) {
	if env := os.Getenv("GOCACHE"); env != "" {
		return filepath.Clean(env), true
	}
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return "", false
	}
	return filepath.Join(dir, "go-build"), true
}

func sameCachePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return false
	}
	return filepath.Clean(ra) == filepath.Clean(rb)
}
