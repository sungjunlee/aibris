package adapter

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// estimateDirSize returns the total file size in bytes for the given path.
// For regular files it returns the file's size directly.
// For directories it uses a concurrent walker that's significantly faster
// than filepath.WalkDir for large trees by eliminating per-file callback
// overhead and walking subdirectories in parallel (bounded goroutine pool).
//
// The walker uses os.ReadDir (batched getdents64) and os.DirEntry.Info()
// for individual file stat — the same underlying syscalls as WalkDir — but
// avoids interface dispatch per entry and exploits goroutine-level
// parallelism for subdirectory traversal.
func estimateDirSize(ctx context.Context, path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.Size()
	}

	var total atomic.Int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16) // max concurrent walkers
	wg.Add(1)
	walkDir(ctx, path, &total, &wg, sem)
	wg.Wait()
	return total.Load()
}

// walkDir recursively walks a single directory, adding file sizes to total.
// Each subdirectory is walked in a new goroutine (bounded by sem).
func walkDir(ctx context.Context, path string, total *atomic.Int64, wg *sync.WaitGroup, sem chan struct{}) {
	defer wg.Done()

	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return
		}

		// Check IsDir() via d_type (no syscall needed) before calling
		// entry.Info() which triggers an lstat per entry.
		if entry.IsDir() {
			// Subdirectory: walk concurrently, bounded by semaphore
			subPath := filepath.Join(path, entry.Name())
			wg.Add(1)
			go func() {
				defer func() {
					<-sem
				}()
				sem <- struct{}{}
				walkDir(ctx, subPath, total, wg, sem)
			}()
		} else {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			total.Add(info.Size())
		}
	}
}

// estimateDirSizeWalkDir is the original single-threaded WalkDir fallback.
func estimateDirSizeWalkDir(ctx context.Context, path string) int64 {
	var total int64
	filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := d.Info()
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
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
