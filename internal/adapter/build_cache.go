package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sungjunlee/aibris/internal/types"
)

type BuildCacheAdapter struct{}

func (a *BuildCacheAdapter) Name() types.Tool {
	return types.ToolBuildCache
}

func (a *BuildCacheAdapter) Category() types.Category {
	return types.CategoryBuildCache
}

func (a *BuildCacheAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
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

	candidates := []struct {
		id      string
		path    string
		os      string
		command []string
	}{
		{id: "go-build", path: filepath.Join(home, ".cache", "go-build"), command: []string{"go", "clean", "-cache"}},
		{id: "xcode", path: filepath.Join(home, "Library", "Caches", "Xcode"), os: "darwin"},
		{id: "xcode-deriveddata", path: filepath.Join(home, "Library", "Developer", "Xcode", "DerivedData"), os: "darwin"},
		{id: "homebrew", path: filepath.Join(home, "Library", "Caches", "Homebrew"), os: "darwin", command: []string{"brew", "cleanup", "--prune=all"}},
		{id: "cocoapods", path: filepath.Join(home, "Library", "Caches", "CocoaPods"), os: "darwin"},
		{id: "gradle", path: filepath.Join(home, ".gradle", "caches")},
		{id: "npm", path: filepath.Join(home, ".npm", "_cacache"), command: []string{"npm", "cache", "clean", "--force"}},
		{id: "cargo", path: filepath.Join(home, ".cargo", "registry")},
		{id: "dart-analysis", path: filepath.Join(home, ".dartServer")},
	}

	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.os != "" && c.os != runtime.GOOS {
			continue
		}
		if !pathUnderRoots(c.path, roots) {
			continue
		}
		info, err := os.Stat(c.path)
		if err != nil || !info.IsDir() {
			continue
		}
		activity := estimateDirActivity(ctx, c.path)
		modTime := info.ModTime()
		if activity.NewestModTime.After(modTime) {
			modTime = activity.NewestModTime
		}
		item := types.DebrisInfo{
			Tool:        types.ToolBuildCache,
			Category:    types.CategoryBuildCache,
			ID:          c.id,
			Path:        c.path,
			Size:        activity.Size,
			ModTime:     modTime,
			PathModTime: info.ModTime(),
		}
		if len(c.command) > 0 {
			item.CleanupKind = types.CleanupCommand
			item.CleanupCommand = c.command
		}
		results = append(results, item)
	}

	return results, nil
}
