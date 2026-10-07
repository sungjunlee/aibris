package adapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// dirActivity reports the total file bytes and the newest modification time
// observed anywhere in a tree. Err is non-nil if any activity evidence is
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
}

// estimateDirSize returns the total file size in bytes for the given path.
// For regular files it returns the file's size directly.
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
				filesSize += info.Size()
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

func estimateDirSizes(ctx context.Context, paths []string) map[string]int64 {
	sizes := make(map[string]int64, len(paths))
	if len(paths) == 0 || ctx.Err() != nil {
		return sizes
	}
	if runtime.GOOS != "windows" {
		if duSizes, ok := estimateDirSizesWithDU(ctx, paths); ok {
			return duSizes
		}
	}
	for _, path := range paths {
		if ctx.Err() != nil {
			break
		}
		sizes[path] = estimateDirSize(ctx, path)
	}
	return sizes
}

func estimateDirSizesWithDU(ctx context.Context, paths []string) (map[string]int64, bool) {
	if _, err := exec.LookPath("du"); err != nil {
		return nil, false
	}
	args := append([]string{"-sk"}, paths...)
	output, err := exec.CommandContext(ctx, "du", args...).Output()
	if err != nil {
		return nil, false
	}
	sizes := make(map[string]int64, len(paths))
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		sizeField, pathField, ok := strings.Cut(line, "\t")
		if !ok {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return nil, false
			}
			sizeField = fields[0]
			pathField = strings.TrimSpace(strings.TrimPrefix(line, sizeField))
		}
		if pathField == "" {
			return nil, false
		}
		kb, err := strconv.ParseInt(sizeField, 10, 64)
		if err != nil {
			return nil, false
		}
		sizes[pathField] = kb * 1024
	}
	if len(sizes) != len(paths) {
		return nil, false
	}
	return sizes, true
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
		total.Add(info.Size())
		if trackModTime {
			activity.recordModTime(info.ModTime())
		}
		return nil
	})
	activity.recordError(err)
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
