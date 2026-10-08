package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RefuseStaleGoCache reports an error when the live GOCACHE path no longer
// matches the verified path recorded at scan time, including its signature
// and Go-only top-level layout.
func RefuseStaleGoCache(planned string) error {
	live, ok := effectiveGoCache()
	if !ok {
		return fmt.Errorf("live GOCACHE path, README signature or top-level layout could not be verified")
	}
	if !sameCachePath(live, planned) {
		return fmt.Errorf("live GOCACHE %q no longer matches planned %q", live, planned)
	}
	return nil
}

func effectiveGoCache() (string, bool) {
	path, overridden := goCacheLocation()
	if path == "" || !hasGoCacheLayout(path) || (overridden && !hasGoCacheREADME(path)) {
		return "", false
	}
	return path, true
}

// goCacheLocation distinguishes explicit GOCACHE settings from the default.
func goCacheLocation() (string, bool) {
	if env := os.Getenv("GOCACHE"); env != "" {
		path, _ := validGoCachePath(env)
		return path, true
	}
	if env, ok := goEnvFileGoCache(); ok {
		path, _ := validGoCachePath(env)
		return path, true
	}
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return "", false
	}
	return filepath.Join(dir, "go-build"), false
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

// goCacheREADMESignature is the first line of cacheREADME in Go's cache:
// https://go.dev/src/cmd/go/internal/cache/default.go
const goCacheREADMESignature = "This directory holds cached build artifacts from the Go build system."

func hasGoCacheREADME(dir string) bool {
	readme := filepath.Join(dir, "README")
	info, err := os.Lstat(readme)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(readme)
	return err == nil && strings.HasPrefix(string(data), goCacheREADMESignature)
}

// hasGoCacheLayout checks only one directory level: a genuine README proves
// Go used the directory, but foreign entries mean it is not exclusively a cache.
// Go creates hex buckets, fuzz, and metadata as described in:
// https://go.dev/src/cmd/go/internal/cache/cache.go
// https://go.dev/src/cmd/go/internal/clean/clean.go
// README is created by internal/cache/default.go (see signature above).
func hasGoCacheLayout(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		info, err := entry.Info() // Does not follow symlinks.
		if err != nil {
			return false
		}
		name := entry.Name()
		switch name {
		// .DS_Store is Finder view metadata, created whenever the directory is
		// opened in Finder; it holds no user data.
		case "README", "trim.txt", "testexpire.txt", "log.txt", ".DS_Store":
			if !info.Mode().IsRegular() {
				return false
			}
		default:
			hexBucket := len(name) == 2 && strings.Trim(name, "0123456789abcdef") == ""
			if !info.IsDir() || (name != "fuzz" && !hexBucket) {
				return false
			}
		}
	}
	return true
}
