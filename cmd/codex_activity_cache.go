package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Cache persist, aggregate, and index conversion for the Codex activity
// index. Load/recommend entrypoints stay in codex_activity.go. Session-file
// walkers stay in codex_activity_helpers.go.

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

func codexActivityCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aibris", "codex-activity.json"), nil
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
		Members:   aggregateCodexMemberActivity(cache.Files),
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
		Members:   make(map[string]codexWorktreeActivity),
		Projects:  make(map[string]codexProjectActivity),
		Err:       err,
	}
}

func aggregateCodexMemberActivity(files map[string]codexActivityFileRecord) map[string]codexWorktreeActivity {
	members := make(map[string]codexWorktreeActivity)
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
		key := codexActivityMemberKey(record.WorktreeID, record.Project)
		activity := members[key]
		activity.WorktreeID = record.WorktreeID
		activity.Project = record.Project
		activity.SessionCount++
		if record.Timestamp.After(activity.LatestSession) {
			activity.LatestSession = record.Timestamp
		}
		members[key] = activity
	}
	return members
}

func codexActivityMemberKey(worktreeID, project string) string {
	return worktreeID + "\x00" + project
}
