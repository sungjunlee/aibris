package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

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
	// signature lists entries, relative to the cache directory, at least one
	// of which a real cache of this kind contains. A directory chosen by an
	// override variable must show one; otherwise an override could turn any
	// ordinary directory into a cleanup target.
	signature []string
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
		return envOr("HOMEBREW_CACHE", filepath.Join(home, "Library", "Caches", "Homebrew"))
	}, signature: []string{"downloads", "api"}, command: []string{"brew", "cleanup", "--prune=all"}},
	{id: "cocoapods", tool: types.ToolBuildCache, locate: darwinOnly("Library", "Caches", "CocoaPods")},
	{id: "gradle", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		base, overridden := envOr("GRADLE_USER_HOME", filepath.Join(home, ".gradle"))
		return filepath.Join(base, "caches"), overridden
	}, signature: []string{"modules-2", "jars-9", "transforms-3", "transforms-4"}},
	{id: "npm", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		root, overridden := npmCacheRoot(home)
		return filepath.Join(root, "_cacache"), overridden
	}, signature: []string{"index-v5", "content-v2"}, command: []string{"npm", "cache", "clean", "--force"}},
	{id: "npx", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		root, overridden := npmCacheRoot(home)
		return filepath.Join(root, "_npx"), overridden
	}, signature: []string{filepath.Join("..", "_cacache")}},
	{id: "cargo", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		base, overridden := envOr("CARGO_HOME", filepath.Join(home, ".cargo"))
		return filepath.Join(base, "registry"), overridden
	}, signature: []string{"index", "cache", "src"}},
	{id: "dart-analysis", tool: types.ToolBuildCache, locate: func(home string) (string, bool) {
		return filepath.Join(home, ".dartServer"), false
	}},
	{id: "pip", tool: types.ToolPipCache, locate: pipCacheDir, signature: []string{"http", "http-v2", "wheels", "selfcheck"}},
	{id: "uv", tool: types.ToolPipCache, locate: uvCacheDir, signature: []string{"CACHEDIR.TAG"}, command: []string{"uv", "cache", "clean"}},
}

// resolve returns the target's cache directory, or "" when it does not apply
// or an override chose a directory without the cache's signature.
func (t cacheTarget) resolve(home string) string {
	path, overridden := t.locate(home)
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	path = filepath.Clean(path)
	if overridden && !hasAnyEntry(path, t.signature) {
		return ""
	}
	return path
}

func hasAnyEntry(dir string, entries []string) bool {
	for _, entry := range entries {
		if _, err := os.Lstat(filepath.Join(dir, entry)); err == nil {
			return true
		}
	}
	return false
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

func npmCacheRoot(home string) (string, bool) {
	for _, name := range []string{"npm_config_cache", "NPM_CONFIG_CACHE"} {
		if value := os.Getenv(name); value != "" && filepath.IsAbs(value) {
			return value, true
		}
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(localAppData(home), "npm-cache"), false
	}
	return filepath.Join(home, ".npm"), false
}

// pipCacheDir follows pip: PIP_CACHE_DIR, else the platform user cache. On
// macOS pip has used both ~/Library/Caches/pip and the XDG location, so the
// first one that exists wins.
func pipCacheDir(home string) (string, bool) {
	if path, overridden := envOr("PIP_CACHE_DIR", ""); overridden {
		return path, true
	}
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
