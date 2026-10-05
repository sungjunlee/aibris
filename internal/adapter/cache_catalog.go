package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

// cacheTarget is one rebuildable cache: where it lives and how it is cleaned.
// Adding a cache means adding an entry here; scan, the cleanup allowlist,
// and command pinning all read this table.
type cacheTarget struct {
	id   string
	tool types.Tool // provider that reports it; also its JSON "tool"
	// locate returns the cache directory for the current environment, or ""
	// when the cache does not apply here, and whether an override variable
	// chose it. It must honor the tool's own overrides so scan and cleanup see
	// the directory the tool uses.
	locate func(home string) (path string, overridden bool)
	// verify must accept a directory chosen by an override variable before it
	// is scanned or allowlisted; otherwise an override could turn an ordinary
	// directory into a cleanup target. Only caches that mark themselves
	// unambiguously (a CACHEDIR.TAG) honor overrides; the rest use their
	// default location only.
	verify func(path string) bool
	// command, when set, is the tool's own cleanup command. It runs with its
	// cache location pinned to the scanned path (see cleaner.cleanupCommandEnv).
	command []string
}

var cacheCatalog = []cacheTarget{
	{id: "go-build", tool: types.ToolBuildCache, locate: func(string) (string, bool) {
		// effectiveGoCache validates GOCACHE itself.
		path, _ := effectiveGoCache()
		return path, false
	}, command: []string{"go", "clean", "-cache"}},
	{id: "xcode", tool: types.ToolBuildCache, locate: darwinOnly("Library", "Caches", "Xcode")},
	{id: "xcode-deriveddata", tool: types.ToolBuildCache, locate: darwinOnly("Library", "Developer", "Xcode", "DerivedData")},
	{id: "homebrew", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		if runtime.GOOS != "darwin" {
			return "", false
		}
		return filepath.Join(home, "Library", "Caches", "Homebrew"), false
	}, command: []string{"brew", "cleanup", "--prune=all"}},
	{id: "cocoapods", tool: types.ToolBuildCache, locate: darwinOnly("Library", "Caches", "CocoaPods")},
	{id: "gradle", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		return filepath.Join(home, ".gradle", "caches"), false
	}},
	{id: "npm", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		root, overridden := npmCacheRoot(home)
		return filepath.Join(root, "_cacache"), overridden
	}, command: []string{"npm", "cache", "clean", "--force"}},
	{id: "npx", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		root, overridden := npmCacheRoot(home)
		return filepath.Join(root, "_npx"), overridden
	}},
	{id: "cargo", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		return filepath.Join(home, ".cargo", "registry"), false
	}},
	{id: "dart-analysis", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		return filepath.Join(home, ".dartServer"), false
	}},
	{id: "pip", tool: types.ToolPipCache, locate: pipCacheDir},
	{id: "uv", tool: types.ToolPipCache, locate: uvCacheDir, verify: hasCacheDirTag, command: []string{"uv", "cache", "clean"}},
}

// resolve returns the target's cache directory, or "" when it does not apply
// or an override chose a directory without the cache's signature.
func (t cacheTarget) resolve(home string) string {
	path, overridden := t.locate(home)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	path = filepath.Clean(path)
	if overridden && (t.verify == nil || !t.verify(path)) {
		return ""
	}
	return path
}

// cacheDirTagSignature starts every valid CACHEDIR.TAG
// (https://bford.info/cachedir/).
const cacheDirTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"

// hasCacheDirTag reports whether dir holds a regular CACHEDIR.TAG file with
// the standard signature, the convention tools use to mark a cache directory.
func hasCacheDirTag(dir string) bool {
	tag := filepath.Join(dir, "CACHEDIR.TAG")
	info, err := os.Lstat(tag)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(tag)
	return err == nil && strings.HasPrefix(string(data), cacheDirTagSignature)
}

// CacheTargetPaths returns every cache directory the catalog resolves to in
// the current environment. The cleanup allowlist accepts exactly these.
func CacheTargetPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var paths []string
	for _, target := range cacheCatalog {
		if path := target.resolve(home); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

// scanCacheCatalog reports the catalog caches owned by one provider tool.
func scanCacheCatalog(ctx context.Context, opts types.ScanOptions, tool types.Tool, category types.Category) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	var results []types.DebrisInfo
	for _, target := range cacheCatalog {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if target.tool != tool {
			continue
		}
		path := target.resolve(home)
		if path == "" || !pathUnderRoots(path, roots) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		activity := estimateDirActivity(ctx, path)
		modTime := info.ModTime()
		if activity.NewestModTime.After(modTime) {
			modTime = activity.NewestModTime
		}
		item := types.DebrisInfo{
			Tool:        tool,
			Category:    category,
			ID:          target.id,
			Path:        path,
			Size:        activity.Size,
			ModTime:     modTime,
			PathModTime: info.ModTime(),
		}
		if len(target.command) > 0 {
			item.CleanupKind = types.CleanupCommand
			item.CleanupCommand = append([]string(nil), target.command...)
		}
		results = append(results, item)
	}
	return results, nil
}

func darwinOnly(parts ...string) func(string) (string, bool) {
	return func(home string) (string, bool) {
		if runtime.GOOS != "darwin" {
			return "", false
		}
		return filepath.Join(append([]string{home}, parts...)...), false
	}
}

// envOr returns an absolute path from the named variable and true, or
// fallback and false.
func envOr(name, fallback string) (string, bool) {
	if value := os.Getenv(name); value != "" && filepath.IsAbs(value) {
		return value, true
	}
	return fallback, false
}

func localAppData(home string) string {
	path, _ := envOr("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	return path
}

func xdgCacheHome(home string) string {
	path, _ := envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	return path
}

// npmCacheRoot is npm's default cache root. npm_config_cache is not
// honored: npm does not mark its cache, so an override could not be told
// apart from an ordinary directory.
func npmCacheRoot(home string) (string, bool) {
	if runtime.GOOS == "windows" {
		return filepath.Join(localAppData(home), "npm-cache"), false
	}
	return filepath.Join(home, ".npm"), false
}

// pipCacheDir is pip's default cache directory. PIP_CACHE_DIR is not honored
// for the same reason as npm_config_cache. On macOS pip has used both
// ~/Library/Caches/pip and the XDG location, so the Library one wins unless
// only the XDG one exists.
func pipCacheDir(home string) (string, bool) {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(localAppData(home), "pip", "Cache"), false
	case "darwin":
		library := filepath.Join(home, "Library", "Caches", "pip")
		xdg := filepath.Join(xdgCacheHome(home), "pip")
		if _, err := os.Stat(library); err != nil {
			if _, err := os.Stat(xdg); err == nil {
				return xdg, false
			}
		}
		return library, false
	default:
		return filepath.Join(xdgCacheHome(home), "pip"), false
	}
}

// uvCacheDir follows uv: UV_CACHE_DIR, else %LOCALAPPDATA%\uv\cache on
// Windows and $XDG_CACHE_HOME/uv or ~/.cache/uv elsewhere, macOS included.
func uvCacheDir(home string) (string, bool) {
	if path, overridden := envOr("UV_CACHE_DIR", ""); overridden {
		return path, true
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(localAppData(home), "uv", "cache"), false
	}
	return filepath.Join(xdgCacheHome(home), "uv"), false
}
