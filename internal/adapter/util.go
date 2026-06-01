package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// estimateDirSize returns the total file size in bytes for the given path.
// For regular files it returns the file's size directly.
// For directories it uses a worker pool that walks top-level subdirectories
// in parallel, with each worker traversing its assigned subtree sequentially
// (no recursive goroutine spawning). This avoids the goroutine explosion that
// occurs with per-directory goroutine spawning on deep, wide trees.
func estimateDirSize(ctx context.Context, path string) int64 {
	if err := ctx.Err(); err != nil {
		return 0
	}

	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return 0
	}

	// Collect subdirectories to be walked in parallel.
	var subdirs []string
	var filesSize int64
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, filepath.Join(path, e.Name()))
		} else {
			info, err := e.Info()
			if err == nil {
				filesSize += info.Size()
			}
		}
	}

	var total atomic.Int64
	total.Add(filesSize)

	// Walk each subdirectory in a worker goroutine (bounded pool).
	// Concurrent walkers: enough to saturate SSD I/O without thrashing.
	const workers = 8
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for _, subdir := range subdirs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{} // acquire
		go func(dir string) {
			defer func() {
				<-sem // release
				wg.Done()
			}()
			walkDirSequential(ctx, dir, &total)
		}(subdir)
	}

	wg.Wait()
	return total.Load()
}

// walkDirSequential walks a directory tree sequentially within a single
// goroutine using filepath.WalkDir. It adds all file sizes to total
// via atomic add.
func walkDirSequential(ctx context.Context, path string, total *atomic.Int64) {
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return filepath.SkipDir
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				total.Add(info.Size())
			}
		}
		return nil
	})
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
		if cleanPath == cleanRoot {
			return true
		}
		rel, err := filepath.Rel(cleanRoot, cleanPath)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}
