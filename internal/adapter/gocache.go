package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		return validGoCachePath(env)
	}
	if env, ok := goEnvFileGoCache(); ok {
		return validGoCachePath(env)
	}
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return "", false
	}
	return filepath.Join(dir, "go-build"), true
}

func validGoCachePath(env string) (string, bool) {
	if env == "off" || !filepath.IsAbs(env) {
		return "", false
	}
	return filepath.Clean(env), true
}

func goEnvFileGoCache() (string, bool) {
	file, ok := goEnvFilePath()
	if !ok {
		return "", false
	}
	val, ok := readGoEnvKey(file, "GOCACHE")
	if !ok || val == "" {
		return "", false
	}
	return val, true
}

func goEnvFilePath() (string, bool) {
	if file := os.Getenv("GOENV"); file != "" {
		if file == "off" {
			return "", false
		}
		return file, true
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "", false
	}
	return filepath.Join(dir, "go", "env"), true
}

func readGoEnvKey(file, key string) (string, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		i := strings.IndexByte(line, '=')
		if i < 0 || line[0] < 'A' || 'Z' < line[0] {
			continue
		}
		if line[:i] != key {
			continue
		}
		return line[i+1:], true
	}
	return "", false
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
