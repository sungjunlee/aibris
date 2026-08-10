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
// observed anywhere in a tree.
type dirActivity struct {
	Size          int64
	NewestModTime time.Time
}

type dirActivityAccumulator struct {
	modTimeMu        sync.Mutex
	newestModTime    time.Time
	hasReadableEntry bool
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

// NewestTreeModTime reports the newest modification time observed anywhere in
// the tree at path, or the zero time when nothing readable was found. It is
// the same signal cache adapters record as ModTime, for callers that must
// re-derive it after the scan.
//
// The walk skips a subtree it cannot read, so an unreadable directory hides
// any newer mtime beneath it and the result can be older than the tree really
// is. Callers must therefore treat this as a lower bound and combine it with
// whatever activity they already recorded, never replace that record with it.
func NewestTreeModTime(ctx context.Context, path string) time.Time {
	return estimateDirActivity(ctx, path).NewestModTime
}

func estimateDirActivityWithOptions(ctx context.Context, path string, trackModTime bool) dirActivity {
	if err := ctx.Err(); err != nil {
		return dirActivity{}
	}

	info, err := os.Stat(path)
	if err != nil {
		return dirActivity{}
	}
	if !info.IsDir() {
		activity := dirActivity{Size: info.Size()}
		if trackModTime {
			activity.NewestModTime = info.ModTime()
		}
		return activity
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return dirActivity{}
	}

	// Collect subdirectories to be walked in parallel.
	var subdirs []string
	var filesSize int64
	activity := &dirActivityAccumulator{}
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, filepath.Join(path, e.Name()))
		} else {
			info, err := e.Info()
			if err == nil {
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
			walkDirSequential(ctx, dir, &total, activity, trackModTime)
		}(subdir)
	}

	wg.Wait()
	result := dirActivity{Size: total.Load()}
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
		if d.IsDir() {
			if trackModTime {
				info, err := d.Info()
				if err == nil {
					activity.recordModTime(info.ModTime())
				}
			}
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total.Add(info.Size())
			if trackModTime {
				activity.recordModTime(info.ModTime())
			}
		}
		return nil
	})
}

func (a *dirActivityAccumulator) recordModTime(modTime time.Time) {
	a.modTimeMu.Lock()
	defer a.modTimeMu.Unlock()
	if !a.hasReadableEntry || modTime.After(a.newestModTime) {
		a.newestModTime = modTime
	}
	a.hasReadableEntry = true
}

func (a *dirActivityAccumulator) latestModTime(rootModTime time.Time) time.Time {
	a.modTimeMu.Lock()
	defer a.modTimeMu.Unlock()
	if !a.hasReadableEntry {
		return time.Time{}
	}
	if rootModTime.After(a.newestModTime) {
		return rootModTime
	}
	return a.newestModTime
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

// projectNameFromRecordedCWD labels a cwd-keyed store without requiring the
// recorded directory to still exist.
func projectNameFromRecordedCWD(path string) string {
	cleanPath := filepath.Clean(path)
	if cleanPath == string(filepath.Separator) || cleanPath == "." {
		return ""
	}
	return filepath.Base(cleanPath)
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
