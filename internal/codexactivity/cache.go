package codexactivity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
)

// Cache persist, aggregate, and index conversion for the Codex activity
// index. Load/recommend entrypoints and session-file walkers stay in index.go.

type Cache struct {
	SchemaVersion int                       `json:"schema_version"`
	CreatedAt     time.Time                 `json:"created_at"`
	SessionRoots  []string                  `json:"session_roots"`
	Sources       map[string]SourceCoverage `json:"sources"`
	Files         map[string]FileRecord     `json:"files"`
	Worktrees     map[string]Worktree       `json:"worktrees"`
	Projects      map[string]Project        `json:"projects"`
}

type FileRecord struct {
	Path       string    `json:"path"`
	Home       string    `json:"home"`
	ModTime    time.Time `json:"mod_time"`
	Size       int64     `json:"size"`
	Valid      bool      `json:"valid"`
	WorktreeID string    `json:"worktree_id,omitempty"`
	Project    string    `json:"project,omitempty"`
	Timestamp  time.Time `json:"timestamp"` // first session_meta start time
}

// activityTime includes work appended to an existing rollout without reading
// conversation records. Timestamp remains the session's start time.
func (r FileRecord) activityTime() time.Time {
	if r.ModTime.After(r.Timestamp) {
		return r.ModTime
	}
	return r.Timestamp
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
	if len(cache.SessionRoots) == 0 || len(cache.Sources) == 0 {
		return Cache{}, false, errors.New("codex activity cache missing source identity")
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
	opts = FillOptions(opts)
	sources := make(map[string]SourceCoverage)
	for _, root := range opts.SessionRoots {
		home := filepath.Dir(root)
		coverage := sources[home]
		coverage.Roots = append(coverage.Roots, root)
		sources[home] = coverage
	}
	records := make(map[string]FileRecord)
	for home, coverage := range sources {
		files, err := findSessionFiles(ctx, coverage.Roots)
		if ctx.Err() != nil {
			return Cache{}, ctx.Err()
		}
		if err != nil {
			coverage.Absent = ownRootsOnly(home, coverage.Roots) && defaultHomeAbsent(home)
			sources[home] = coverage
			continue
		}
		// Requested roots must be readable; both live store leaves must also pass
		// validation below. Proven default-home absence is recorded separately.
		activeRoot := canonicalPath(filepath.Join(home, "sessions"))
		for _, root := range coverage.Roots {
			if info, err := os.Stat(root); err == nil && info.IsDir() {
				coverage.Available = true
				if root == activeRoot {
					coverage.ActiveRoot = true
				}
			}
		}
		homeRecords := make(map[string]FileRecord)
		for _, file := range files {
			if err := ctx.Err(); err != nil {
				return Cache{}, err
			}
			file.home = home
			if previousOK && sameRoots(previous.SessionRoots, opts.SessionRoots) {
				if cached, ok := previous.Files[file.path]; ok && cached.Home == home && cached.ModTime.Equal(file.modTime) && cached.Size == file.size {
					homeRecords[file.path] = cached
					continue
				}
			}
			record, err := readSessionFileRecord(ctx, file)
			if err != nil {
				coverage.Available = false
				break
			}
			homeRecords[file.path] = record
		}
		coverage.Available = coverage.Available && homeStoresAvailable(home)
		// Compute absence after the walk: another root grouped under the default
		// home may have records even when its two standard stores are absent.
		coverage.Absent = len(homeRecords) == 0 && ownRootsOnly(home, coverage.Roots) && defaultHomeAbsent(home)
		sources[home] = coverage
		if coverage.Available {
			for path, record := range homeRecords {
				records[path] = record
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Cache{}, err
	}
	cache := Cache{
		SchemaVersion: CacheSchemaVersion,
		CreatedAt:     opts.Now,
		SessionRoots:  opts.SessionRoots,
		Sources:       sources,
		Files:         records,
	}
	cache.rebuildAggregates()
	return cache, nil
}

// defaultHomeAbsent proves absence without following home or store symlinks.
// I/O failures and explicit configuration cannot supply negative evidence.
// ownRootsOnly reports whether every grouped root is one of the home's own two
// stores, so a foreign root grouped under the home cannot be called absent.
func ownRootsOnly(home string, roots []string) bool {
	own := canonicalRoots([]string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")})
	for _, root := range roots {
		if !slices.Contains(own, root) {
			return false
		}
	}
	return true
}

func defaultHomeAbsent(home string) bool {
	defaultHome, err := codexhome.UnconfiguredDefaultHome()
	if err != nil || defaultHome == "" || canonicalPath(defaultHome) != home {
		return false
	}
	for _, configured := range codexhome.ExtraHomes() {
		if canonicalPath(configured) == home {
			return false
		}
	}
	info, err := os.Lstat(defaultHome)
	if err != nil {
		return errors.Is(err, os.ErrNotExist)
	}
	if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return false
	}
	for _, store := range []string{"sessions", "archived_sessions"} {
		if _, err := os.Lstat(filepath.Join(defaultHome, store)); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
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
		activityTime := record.activityTime()

		worktree := worktrees[WorktreeKey(record.Home, record.WorktreeID)]
		worktree.WorktreeID = record.WorktreeID
		if worktree.Project == "" {
			worktree.Project = record.Project
		}
		worktree.SessionCount++
		if activityTime.After(worktree.LatestSession) {
			worktree.LatestSession = activityTime
		}
		worktrees[WorktreeKey(record.Home, record.WorktreeID)] = worktree

		project := projects[ProjectKey(record.Home, record.Project)]
		project.Project = record.Project
		project.SessionCount++
		if activityTime.After(project.LatestSession) {
			project.LatestSession = activityTime
		}
		projects[ProjectKey(record.Home, record.Project)] = project
	}
	return worktrees, projects
}

func indexFromCache(cache Cache, age time.Duration, source string, err error) Index {
	cache.rebuildAggregates()
	index := Index{
		Available: false,
		Sources:   cache.Sources,
		Source:    source,
		Age:       age,
		Worktrees: cache.Worktrees,
		Members:   aggregateMembers(cache.Files),
		Projects:  cache.Projects,
		Err:       err,
	}
	for _, coverage := range cache.Sources {
		index.Available = index.Available || coverage.Available
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
		key := MemberKey(record.Home, record.WorktreeID, record.Project)
		activityTime := record.activityTime()
		activity := members[key]
		activity.WorktreeID = record.WorktreeID
		activity.Project = record.Project
		activity.SessionCount++
		if activityTime.After(activity.LatestSession) {
			activity.LatestSession = activityTime
		}
		members[key] = activity
	}
	return members
}

func WorktreeKey(home, worktreeID string) string {
	return home + "\x00" + worktreeID
}

func ProjectKey(home, project string) string {
	return home + "\x00" + project
}

func MemberKey(home, worktreeID, project string) string {
	return WorktreeKey(home, worktreeID) + "\x00" + project
}
