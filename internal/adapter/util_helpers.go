package adapter

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// dirActivity reports apparent bytes (the DebrisInfo.Size contract) and the
// newest modification time observed anywhere in a tree. Err is non-nil if any activity evidence is
// missing; Size and NewestModTime remain partial, report-only observations.
type dirActivity struct {
	Size          int64
	NewestModTime time.Time
	Err           error
}

var walkDirectory = filepath.WalkDir

type dirActivityAccumulator struct {
	mu               sync.Mutex
	newestModTime    time.Time
	hasReadableEntry bool
	err              error
	seenHardlinks    map[sizeFileIdentity]struct{}
}

type sizeFileIdentity struct {
	device uint64
	inode  uint64
}

// estimateDirSize returns apparent bytes under the DebrisInfo.Size contract:
// directories contribute no bytes and regular hardlinks count once per device
// and inode within this tree when identity is available, otherwise per path.
// Root symlinks are followed; nested symlinks contribute their own length
// without following their targets. Unreadable entries leave a partial
// report-only size.
// For directories it uses a worker pool that walks top-level subdirectories
// in parallel, with each worker traversing its assigned subtree sequentially
// (no recursive goroutine spawning). This avoids the goroutine explosion that
// occurs with per-directory goroutine spawning on deep, wide trees.
func estimateDirSize(ctx context.Context, path string) int64 {
	return estimateDirActivityWithOptions(ctx, path, false).Size
}

func estimateDirActivity(ctx context.Context, path string) dirActivity {
	return estimateDirActivityWithOptions(ctx, path, true)
}

func estimateDirActivityWithOptions(ctx context.Context, path string, trackModTime bool) dirActivity {
	if err := ctx.Err(); err != nil {
		return dirActivity{Err: err}
	}

	// Cache discovery accepts directory symlinks. Observe the same root target
	// so size and safety-critical activity evidence describe its contents.
	info, err := os.Stat(path)
	if err != nil {
		return dirActivity{Err: err}
	}
	if !info.IsDir() {
		activity := dirActivity{Size: info.Size(), Err: ctx.Err()}
		if trackModTime {
			activity.NewestModTime = info.ModTime()
		}
		return activity
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return dirActivity{Err: err}
	}

	// Collect subdirectories to be walked in parallel.
	var subdirs []string
	var filesSize int64
	activity := &dirActivityAccumulator{}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			activity.recordError(err)
			break
		}
		if e.IsDir() {
			subdirs = append(subdirs, filepath.Join(path, e.Name()))
		} else {
			info, err := e.Info()
			if err != nil {
				activity.recordError(err)
			} else {
				filesSize += activity.fileSize(info)
				if trackModTime {
					activity.recordModTime(info.ModTime())
				}
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

schedule:
	for _, subdir := range subdirs {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}: // acquire
		case <-ctx.Done():
			break schedule
		}
		wg.Add(1)
		go func(dir string) {
			defer func() {
				<-sem // release
				wg.Done()
			}()
			walkDirSequential(ctx, dir, &total, activity, trackModTime)
		}(subdir)
	}

	wg.Wait()
	activity.recordError(ctx.Err())
	result := dirActivity{Size: total.Load(), Err: activity.err}
	if trackModTime {
		result.NewestModTime = activity.latestModTime(info.ModTime())
	}
	return result
}

// estimateDirSizes measures each target independently with the same walker.
// du is intentionally not used: portable du flags cannot match apparent bytes
// with this hardlink policy and no directory metadata bytes.
func estimateDirSizes(ctx context.Context, paths []string) map[string]int64 {
	sizes := make(map[string]int64, len(paths))
	if len(paths) == 0 || ctx.Err() != nil {
		return sizes
	}
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		sizes[path] = estimateDirSize(ctx, path)
	}
	return sizes
}

// walkDirSequential walks a directory tree sequentially within a single
// goroutine using filepath.WalkDir. It adds all file sizes to total
// via atomic add and optionally records modification times.
func walkDirSequential(
	ctx context.Context,
	path string,
	total *atomic.Int64,
	activity *dirActivityAccumulator,
	trackModTime bool,
) {
	err := walkDirectory(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			activity.recordError(err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// Missing evidence refuses safety approval, but reporting should
			// still collect readable siblings instead of abandoning the walk.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if trackModTime {
				info, err := d.Info()
				if err != nil {
					activity.recordError(err)
				} else {
					activity.recordModTime(info.ModTime())
				}
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			activity.recordError(err)
			return nil
		}
		total.Add(activity.fileSize(info))
		if trackModTime {
			activity.recordModTime(info.ModTime())
		}
		return nil
	})
	activity.recordError(err)
}

// fileSize deduplicates only regular files with multiple links and available
// identity. The set is shared by all workers in one measured tree and allocated
// lazily, so ordinary files and unknown identities need no bookkeeping.
func (a *dirActivityAccumulator) fileSize(info os.FileInfo) int64 {
	identity, ok := hardlinkSizeIdentity(info)
	if !ok {
		return info.Size()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, seen := a.seenHardlinks[identity]; seen {
		return 0
	}
	if a.seenHardlinks == nil {
		a.seenHardlinks = make(map[sizeFileIdentity]struct{})
	}
	a.seenHardlinks[identity] = struct{}{}
	return info.Size()
}

func (a *dirActivityAccumulator) recordError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err == nil {
		a.err = err
	}
}

func (a *dirActivityAccumulator) recordModTime(modTime time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.hasReadableEntry || modTime.After(a.newestModTime) {
		a.newestModTime = modTime
	}
	a.hasReadableEntry = true
}

func (a *dirActivityAccumulator) latestModTime(rootModTime time.Time) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.hasReadableEntry {
		return time.Time{}
	}
	if rootModTime.After(a.newestModTime) {
		return rootModTime
	}
	return a.newestModTime
}
