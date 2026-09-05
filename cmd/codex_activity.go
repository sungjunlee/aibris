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

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	codexActivityCacheSchemaVersion = 1
	codexActivityFreshness          = 15 * time.Minute

	codexActivitySourceCache       = "cache"
	codexActivitySourceRefresh     = "refresh"
	codexActivitySourceUnavailable = "unavailable"

	codexActivityProtectionUnavailable = "codex activity unavailable"
	codexActivityProtectionActive      = "active worktree protected"
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
	Members   map[string]codexWorktreeActivity
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

type codexActivityRecommendationPlan struct {
	Activity        codexActivityIndex
	Recommendations []codexActivityRecommendation
	ProtectedCount  int
	ProtectedSize   int64
}

type codexActivityRecommendation struct {
	Item      types.DebrisInfo
	Protected bool
	Reason    string
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

func loadCodexActivityRecommendations(ctx context.Context, items []types.DebrisInfo) codexActivityRecommendationPlan {
	candidates := activeCodexWorktrees(items)
	if len(candidates) == 0 {
		return codexActivityRecommendationPlan{}
	}
	return recommendCodexActivityWorktrees(candidates, loadCodexActivityIndex(ctx))
}

func recommendCodexActivityWorktrees(items []types.DebrisInfo, activity codexActivityIndex) codexActivityRecommendationPlan {
	plan := codexActivityRecommendationPlan{Activity: activity}
	for _, item := range items {
		if !isActiveCodexWorktree(item) {
			continue
		}
		recommendation := codexActivityRecommendation{
			Item:      item,
			Protected: true,
			Reason:    codexActivityProtectionActive,
		}
		if !activity.Available {
			recommendation.Reason = codexActivityProtectionUnavailable
		}
		plan.Recommendations = append(plan.Recommendations, recommendation)
		if recommendation.Protected {
			plan.ProtectedCount++
			plan.ProtectedSize += item.Size
		}
	}
	return plan
}

func activeCodexWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		if isActiveCodexWorktree(item) {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

// activeWorktrees admits every tool's active worktree units. Guided review is
// built on Git evidence, which every worktree carries; the Codex activity
// index refines a decision but is not what makes a row possible.
func activeWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		if item.Category == types.CategoryWorktree && item.Status == types.WorktreeActive {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

func isActiveCodexWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Tool == types.ToolCodex &&
		item.Status == types.WorktreeActive
}

func codexActivityCachePath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aibris", "codex-activity.json"), nil
}

// defaultCodexSessionRoots returns the Codex session roots under the
// resolved Codex home ($CODEX_HOME, or ~/.codex when unset).
func defaultCodexSessionRoots() ([]string, error) {
	codexHome, err := codexhome.Home()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(codexHome, "sessions"),
		filepath.Join(codexHome, "archived_sessions"),
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
