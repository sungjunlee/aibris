package codexactivity

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
// index. Load/recommend entrypoints and session-file walkers stay in index.go.

type Cache struct {
	SchemaVersion int                   `json:"schema_version"`
	CreatedAt     time.Time             `json:"created_at"`
	Files         map[string]FileRecord `json:"files"`
	Worktrees     map[string]Worktree   `json:"worktrees"`
	Projects      map[string]Project    `json:"projects"`
}

type FileRecord struct {
	Path       string    `json:"path"`
	ModTime    time.Time `json:"mod_time"`
	Size       int64     `json:"size"`
	Valid      bool      `json:"valid"`
	WorktreeID string    `json:"worktree_id,omitempty"`
	Project    string    `json:"project,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

func CachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aibris", "codex-activity.json"), nil
}

func Read(path string) (Cache, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cache{}, false, nil
		}
		return Cache{}, false, err
	}
	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return Cache{}, false, err
	}
	if cache.SchemaVersion != CacheSchemaVersion {
		return Cache{}, false, fmt.Errorf("unsupported codex activity schema version %d", cache.SchemaVersion)
	}
	if cache.CreatedAt.IsZero() {
		return Cache{}, false, errors.New("codex activity cache missing created_at")
	}
	if cache.Files == nil {
		cache.Files = make(map[string]FileRecord)
	}
	return cache, true, nil
}

func Save(path string, cache Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func Refresh(ctx context.Context, opts IndexOptions, previous Cache, previousOK bool) (Cache, error) {
	files, err := findSessionFiles(ctx, opts.SessionRoots)
	if err != nil {
		return Cache{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	records := make(map[string]FileRecord, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return Cache{}, err
		}
		if previousOK {
			if cached, ok := previous.Files[file.path]; ok && cached.ModTime.Equal(file.modTime) && cached.Size == file.size {
				cached.Path = file.path
				records[file.path] = cached
				continue
			}
		}
		record, err := readSessionFileRecord(file)
		if err != nil {
			return Cache{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		records[file.path] = record
	}

	cache := Cache{
		SchemaVersion: CacheSchemaVersion,
		CreatedAt:     opts.Now,
		Files:         records,
	}
	cache.rebuildAggregates()
	return cache, nil
}

func (c *Cache) rebuildAggregates() {
	if c.Files == nil {
		c.Files = make(map[string]FileRecord)
	}
	c.Worktrees, c.Projects = aggregate(c.Files)
}

func aggregate(files map[string]FileRecord) (map[string]Worktree, map[string]Project) {
	worktrees := make(map[string]Worktree)
	projects := make(map[string]Project)
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

func indexFromCache(cache Cache, age time.Duration, source string, err error) Index {
	cache.rebuildAggregates()
	index := Index{
		Available: len(cache.Worktrees) > 0,
		Source:    source,
		Age:       age,
		Worktrees: cache.Worktrees,
		Members:   aggregateMembers(cache.Files),
		Projects:  cache.Projects,
		Err:       err,
	}
	if !index.Available && index.Err == nil {
		index.Err = fmt.Errorf("%w: no indexed session metadata", ErrUnavailable)
	}
	if !index.Available {
		index.Source = SourceUnavailable
	}
	return index
}

func Unavailable(err error) Index {
	if err == nil {
		err = ErrUnavailable
	}
	return Index{
		Available: false,
		Source:    SourceUnavailable,
		Worktrees: make(map[string]Worktree),
		Members:   make(map[string]Worktree),
		Projects:  make(map[string]Project),
		Err:       err,
	}
}

func aggregateMembers(files map[string]FileRecord) map[string]Worktree {
	members := make(map[string]Worktree)
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
		key := MemberKey(record.WorktreeID, record.Project)
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

func MemberKey(worktreeID, project string) string {
	return worktreeID + "\x00" + project
}
