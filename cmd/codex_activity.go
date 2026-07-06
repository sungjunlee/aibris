package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	codexActivityCacheSchemaVersion = 1
	codexActivityFreshness          = 15 * time.Minute

	codexActivitySourceCache       = "cache"
	codexActivitySourceRefresh     = "refresh"
	codexActivitySourceUnavailable = "unavailable"
)

var errCodexActivityUnavailable = errors.New("codex activity unavailable")

type codexActivityIndexOptions struct {
	now          time.Time
	cachePath    string
	sessionRoots []string
	freshness    time.Duration
}

type codexActivityIndex struct {
	Available bool
	Source    string
	Age       time.Duration
	Worktrees map[string]codexWorktreeActivity
	Projects  map[string]codexProjectActivity
	Err       error
}

type codexWorktreeActivity struct {
	WorktreeID    string    `json:"worktree_id"`
	Project       string    `json:"project"`
	SessionCount  int       `json:"session_count"`
	LatestSession time.Time `json:"latest_session"`
}

type codexProjectActivity struct {
	Project       string    `json:"project"`
	SessionCount  int       `json:"session_count"`
	LatestSession time.Time `json:"latest_session"`
}

type codexActivityCache struct {
	SchemaVersion int                                `json:"schema_version"`
	CreatedAt     time.Time                          `json:"created_at"`
	Files         map[string]codexActivityFileRecord `json:"files"`
	Worktrees     map[string]codexWorktreeActivity   `json:"worktrees"`
	Projects      map[string]codexProjectActivity    `json:"projects"`
}

type codexActivityFileRecord struct {
	Path       string    `json:"path"`
	ModTime    time.Time `json:"mod_time"`
	Size       int64     `json:"size"`
	Valid      bool      `json:"valid"`
	WorktreeID string    `json:"worktree_id,omitempty"`
	Project    string    `json:"project,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

type codexSessionFileInfo struct {
	path    string
	modTime time.Time
	size    int64
}

func loadCodexActivityIndex(ctx context.Context) codexActivityIndex {
	return loadCodexActivityIndexWithOptions(ctx, codexActivityIndexOptions{})
}

func loadCodexActivityIndexWithOptions(ctx context.Context, opts codexActivityIndexOptions) codexActivityIndex {
	if err := ctx.Err(); err != nil {
		return unavailableCodexActivityIndex(err)
	}
	opts = fillCodexActivityIndexOptions(opts)
	if opts.cachePath == "" {
		return unavailableCodexActivityIndex(fmt.Errorf("%w: cache path unavailable", errCodexActivityUnavailable))
	}
	if len(opts.sessionRoots) == 0 {
		return unavailableCodexActivityIndex(fmt.Errorf("%w: session roots unavailable", errCodexActivityUnavailable))
	}

	cache, cacheOK, cacheErr := readCodexActivityCache(opts.cachePath)
	if cacheOK {
		cache.rebuildAggregates()
		age := opts.now.Sub(cache.CreatedAt)
		if age >= 0 && age <= opts.freshness {
			return indexFromCodexActivityCache(cache, age, codexActivitySourceCache, nil)
		}
	}

	refreshed, err := refreshCodexActivityCache(ctx, opts, cache, cacheOK)
	if err != nil {
		if cacheErr != nil {
			err = errors.Join(cacheErr, err)
		}
		return unavailableCodexActivityIndex(err)
	}
	if err := saveCodexActivityCache(opts.cachePath, refreshed); err != nil {
		return unavailableCodexActivityIndex(fmt.Errorf("%w: %v", errCodexActivityUnavailable, err))
	}
	return indexFromCodexActivityCache(refreshed, 0, codexActivitySourceRefresh, nil)
}

func fillCodexActivityIndexOptions(opts codexActivityIndexOptions) codexActivityIndexOptions {
	if opts.now.IsZero() {
		opts.now = time.Now()
	}
	if opts.freshness == 0 {
		opts.freshness = codexActivityFreshness
	}
	if opts.cachePath == "" {
		if path, err := codexActivityCachePath(); err == nil {
			opts.cachePath = path
		}
	}
	if opts.sessionRoots == nil {
		if roots, err := defaultCodexSessionRoots(); err == nil {
			opts.sessionRoots = roots
		}
	}
	return opts
}

func (i codexActivityIndex) ProjectHasSessionAfter(project string, ts time.Time) bool {
	if !i.Available || project == "" {
		return false
	}
	activity, ok := i.Projects[project]
	return ok && activity.LatestSession.After(ts)
}

func codexActivityCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aibris", "codex-activity.json"), nil
}

func defaultCodexSessionRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(home, ".codex", "sessions"),
		filepath.Join(home, ".codex", "archived_sessions"),
	}, nil
}

func readCodexActivityCache(path string) (codexActivityCache, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return codexActivityCache{}, false, nil
		}
		return codexActivityCache{}, false, err
	}
	var cache codexActivityCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return codexActivityCache{}, false, err
	}
	if cache.SchemaVersion != codexActivityCacheSchemaVersion {
		return codexActivityCache{}, false, fmt.Errorf("unsupported codex activity schema version %d", cache.SchemaVersion)
	}
	if cache.CreatedAt.IsZero() {
		return codexActivityCache{}, false, errors.New("codex activity cache missing created_at")
	}
	if cache.Files == nil {
		cache.Files = make(map[string]codexActivityFileRecord)
	}
	return cache, true, nil
}

func saveCodexActivityCache(path string, cache codexActivityCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func refreshCodexActivityCache(ctx context.Context, opts codexActivityIndexOptions, previous codexActivityCache, previousOK bool) (codexActivityCache, error) {
	files, err := findCodexSessionFiles(ctx, opts.sessionRoots)
	if err != nil {
		return codexActivityCache{}, fmt.Errorf("%w: %v", errCodexActivityUnavailable, err)
	}

	records := make(map[string]codexActivityFileRecord, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return codexActivityCache{}, err
		}
		if previousOK {
			if cached, ok := previous.Files[file.path]; ok && cached.ModTime.Equal(file.modTime) && cached.Size == file.size {
				cached.Path = file.path
				records[file.path] = cached
				continue
			}
		}
		record, err := readCodexSessionFileRecord(file)
		if err != nil {
			return codexActivityCache{}, fmt.Errorf("%w: %v", errCodexActivityUnavailable, err)
		}
		records[file.path] = record
	}

	cache := codexActivityCache{
		SchemaVersion: codexActivityCacheSchemaVersion,
		CreatedAt:     opts.now,
		Files:         records,
	}
	cache.rebuildAggregates()
	return cache, nil
}

func findCodexSessionFiles(ctx context.Context, roots []string) ([]codexSessionFileInfo, error) {
	seen := make(map[string]bool)
	var files []codexSessionFileInfo
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(root), ".jsonl") {
				files = appendSessionFileInfo(files, seen, root, info)
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files = appendSessionFileInfo(files, seen, path, info)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})
	return files, nil
}

func appendSessionFileInfo(files []codexSessionFileInfo, seen map[string]bool, path string, info fs.FileInfo) []codexSessionFileInfo {
	cleanPath := filepath.Clean(path)
	if seen[cleanPath] {
		return files
	}
	seen[cleanPath] = true
	return append(files, codexSessionFileInfo{
		path:    cleanPath,
		modTime: info.ModTime(),
		size:    info.Size(),
	})
}

func readCodexSessionFileRecord(file codexSessionFileInfo) (codexActivityFileRecord, error) {
	record := codexActivityFileRecord{
		Path:    file.path,
		ModTime: file.modTime,
		Size:    file.size,
	}
	f, err := os.Open(file.path)
	if err != nil {
		return record, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return record, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return record, nil
	}

	var meta struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			CWD       string `json:"cwd"`
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
			ThreadID  string `json:"thread_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(line), &meta); err != nil {
		return record, nil
	}
	if meta.Type != "session_meta" {
		return record, nil
	}
	sessionID := firstNonEmpty(meta.Payload.SessionID, meta.Payload.ID, meta.Payload.ThreadID)
	if sessionID == "" || meta.Timestamp == "" || meta.Payload.CWD == "" {
		return record, nil
	}
	timestamp, err := time.Parse(time.RFC3339Nano, meta.Timestamp)
	if err != nil {
		return record, nil
	}
	worktreeID, project, ok := codexActivityWorktreeFromCWD(meta.Payload.CWD)
	if !ok {
		return record, nil
	}

	record.Valid = true
	record.WorktreeID = worktreeID
	record.Project = project
	record.Timestamp = timestamp
	return record, nil
}

func (c *codexActivityCache) rebuildAggregates() {
	if c.Files == nil {
		c.Files = make(map[string]codexActivityFileRecord)
	}
	c.Worktrees, c.Projects = aggregateCodexActivity(c.Files)
}

func aggregateCodexActivity(files map[string]codexActivityFileRecord) (map[string]codexWorktreeActivity, map[string]codexProjectActivity) {
	worktrees := make(map[string]codexWorktreeActivity)
	projects := make(map[string]codexProjectActivity)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		record := files[path]
		if !record.Valid || record.WorktreeID == "" || record.Project == "" || record.Timestamp.IsZero() {
			continue
		}

		worktree := worktrees[record.WorktreeID]
		worktree.WorktreeID = record.WorktreeID
		if worktree.Project == "" {
			worktree.Project = record.Project
		}
		worktree.SessionCount++
		if record.Timestamp.After(worktree.LatestSession) {
			worktree.LatestSession = record.Timestamp
		}
		worktrees[record.WorktreeID] = worktree

		project := projects[record.Project]
		project.Project = record.Project
		project.SessionCount++
		if record.Timestamp.After(project.LatestSession) {
			project.LatestSession = record.Timestamp
		}
		projects[record.Project] = project
	}
	return worktrees, projects
}

func indexFromCodexActivityCache(cache codexActivityCache, age time.Duration, source string, err error) codexActivityIndex {
	cache.rebuildAggregates()
	index := codexActivityIndex{
		Available: len(cache.Worktrees) > 0,
		Source:    source,
		Age:       age,
		Worktrees: cache.Worktrees,
		Projects:  cache.Projects,
		Err:       err,
	}
	if !index.Available && index.Err == nil {
		index.Err = fmt.Errorf("%w: no indexed session metadata", errCodexActivityUnavailable)
	}
	if !index.Available {
		index.Source = codexActivitySourceUnavailable
	}
	return index
}

func unavailableCodexActivityIndex(err error) codexActivityIndex {
	if err == nil {
		err = errCodexActivityUnavailable
	}
	return codexActivityIndex{
		Available: false,
		Source:    codexActivitySourceUnavailable,
		Worktrees: make(map[string]codexWorktreeActivity),
		Projects:  make(map[string]codexProjectActivity),
		Err:       err,
	}
}

func codexActivityWorktreeFromCWD(cwd string) (string, string, bool) {
	parts := pathParts(cwd)
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] != ".codex" || !isCodexActivityWorktreeRoot(parts[i+1]) {
			continue
		}
		worktreeID := parts[i+2]
		project := worktreeID
		if i+3 < len(parts) {
			project = parts[i+3]
		}
		if worktreeID == "" || project == "" {
			return "", "", false
		}
		return worktreeID, project, true
	}
	return "", "", false
}

func pathParts(path string) []string {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if volume != "" {
		clean = strings.TrimPrefix(clean, volume)
	}
	raw := strings.Split(clean, string(os.PathSeparator))
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

func isCodexActivityWorktreeRoot(name string) bool {
	return name == "worktree" ||
		name == "worktrees" ||
		strings.HasPrefix(name, "worktree-") ||
		strings.HasPrefix(name, "worktrees-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
