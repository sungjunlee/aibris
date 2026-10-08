package codexactivity

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/codexsession"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	CacheSchemaVersion = 4 // recognize Orca workspace CWDs in Orca's Codex home
	Freshness          = 15 * time.Minute

	SourceCache       = "cache"
	SourceRefresh     = "refresh"
	SourceUnavailable = "unavailable"

	ProtectionUnavailable = "codex activity unavailable"
	ProtectionActive      = "active worktree protected"
)

var ErrUnavailable = errors.New("codex activity unavailable")

type IndexOptions struct {
	Now          time.Time
	CachePath    string
	SessionRoots []string
	Freshness    time.Duration
}

type Index struct {
	Available bool
	Source    string
	Age       time.Duration
	Worktrees map[string]Worktree
	Members   map[string]Worktree
	Projects  map[string]Project
	Sources   map[string]SourceCoverage
	Err       error
}

type Worktree struct {
	WorktreeID    string    `json:"worktree_id"`
	Project       string    `json:"project"`
	SessionCount  int       `json:"session_count"`
	LatestSession time.Time `json:"latest_session"`
}

type Project struct {
	Project       string    `json:"project"`
	SessionCount  int       `json:"session_count"`
	LatestSession time.Time `json:"latest_session"`
}

type RecommendationPlan struct {
	Activity        Index
	Recommendations []Recommendation
	ProtectedCount  int
	ProtectedSize   int64
}

type Recommendation struct {
	Item      types.DebrisInfo
	Protected bool
	Reason    string
}

func Load(ctx context.Context) Index {
	return LoadWithOptions(ctx, IndexOptions{})
}

func LoadWithOptions(ctx context.Context, opts IndexOptions) Index {
	if err := ctx.Err(); err != nil {
		return Unavailable(err)
	}
	opts = FillOptions(opts)
	if opts.CachePath == "" {
		return Unavailable(fmt.Errorf("%w: cache path unavailable", ErrUnavailable))
	}
	if len(opts.SessionRoots) == 0 {
		return Unavailable(fmt.Errorf("%w: session roots unavailable", ErrUnavailable))
	}

	cache, cacheOK, cacheErr := Read(opts.CachePath)
	if cacheOK && !sameRoots(cache.SessionRoots, opts.SessionRoots) {
		cacheOK = false
	}
	if cacheOK {
		cache.rebuildAggregates()
		age := opts.Now.Sub(cache.CreatedAt)
		if age >= 0 && age <= opts.Freshness {
			return indexFromCache(cache, age, SourceCache, nil)
		}
	}

	refreshed, err := Refresh(ctx, opts, cache, cacheOK)
	if err != nil {
		if cacheErr != nil {
			err = errors.Join(cacheErr, err)
		}
		return Unavailable(err)
	}
	if err := Save(opts.CachePath, refreshed); err != nil {
		return Unavailable(fmt.Errorf("%w: %v", ErrUnavailable, err))
	}
	return indexFromCache(refreshed, 0, SourceRefresh, nil)
}

func FillOptions(opts IndexOptions) IndexOptions {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Freshness == 0 {
		opts.Freshness = Freshness
	}
	if opts.CachePath == "" {
		if path, err := CachePath(); err == nil {
			opts.CachePath = path
		}
	}
	if opts.SessionRoots == nil {
		if roots, err := DefaultSessionRoots(); err == nil {
			opts.SessionRoots = roots
		}
	}
	opts.SessionRoots = canonicalRoots(opts.SessionRoots)
	return opts
}

func (i Index) ProjectHasSessionAfter(home, project string, ts time.Time) bool {
	if !i.Available || project == "" {
		return false
	}
	activity, ok := i.Projects[ProjectKey(canonicalPath(home), project)]
	return ok && activity.LatestSession.After(ts)
}

func LoadRecommendations(ctx context.Context, items []types.DebrisInfo) RecommendationPlan {
	candidates := ActiveCodexWorktrees(items)
	if len(candidates) == 0 {
		return RecommendationPlan{}
	}
	return Recommend(candidates, Load(ctx))
}

func Recommend(items []types.DebrisInfo, activity Index) RecommendationPlan {
	plan := RecommendationPlan{Activity: activity}
	for _, item := range items {
		if !IsActiveCodexWorktree(item) {
			continue
		}
		recommendation := Recommendation{
			Item:      item,
			Protected: true,
			Reason:    ProtectionActive,
		}
		if !activity.Available {
			recommendation.Reason = ProtectionUnavailable
		}
		plan.Recommendations = append(plan.Recommendations, recommendation)
		if recommendation.Protected {
			plan.ProtectedCount++
			plan.ProtectedSize += item.Size
		}
	}
	return plan
}

func ActiveCodexWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		if IsActiveCodexWorktree(item) {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

func IsActiveCodexWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Tool == types.ToolCodex &&
		item.Status == types.WorktreeActive
}

// DefaultSessionRoots uses the same resolved home list as worktree discovery.
func DefaultSessionRoots() ([]string, error) {
	homes, err := codexhome.Homes()
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, home := range homes {
		roots = append(roots, filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions"))
	}
	return roots, nil
}

// Session-file discovery, record parsing, and CWD worktree identity for the
// Codex activity index. Cache I/O and aggregation live in cache.go.

type sessionFileInfo struct {
	path    string
	home    string
	modTime time.Time
	size    int64
}

func findSessionFiles(ctx context.Context, roots []string) ([]sessionFileInfo, error) {
	seen := make(map[string]bool)
	var files []sessionFileInfo
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
			return nil, fmt.Errorf("session root is not a directory")
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
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

func appendSessionFileInfo(files []sessionFileInfo, seen map[string]bool, path string, info fs.FileInfo) []sessionFileInfo {
	if !info.Mode().IsRegular() {
		return files
	}
	cleanPath := filepath.Clean(path)
	if seen[cleanPath] {
		return files
	}
	seen[cleanPath] = true
	return append(files, sessionFileInfo{
		path:    cleanPath,
		modTime: info.ModTime(),
		size:    info.Size(),
	})
}

func readSessionFileRecord(ctx context.Context, file sessionFileInfo) (FileRecord, error) {
	record := FileRecord{Path: file.path, Home: file.home, ModTime: file.modTime, Size: file.size}
	if err := ctx.Err(); err != nil {
		return record, err
	}
	before, err := os.Lstat(file.path)
	if err != nil {
		return record, err
	}
	if !before.Mode().IsRegular() {
		return record, nil
	}
	f, err := os.Open(file.path)
	if err != nil {
		return record, err
	}
	defer func() { _ = f.Close() }()
	opened, err := f.Stat()
	if err != nil {
		return record, err
	}
	after, err := os.Lstat(file.path)
	if err != nil {
		return record, err
	}
	if !opened.Mode().IsRegular() || !after.Mode().IsRegular() || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
		return record, fmt.Errorf("%w: session leaf changed while opening", ErrUnavailable)
	}
	metadata, err := codexsession.ReadFirstMetadataFrom(ctx, f)
	if err != nil {
		return record, err
	}
	if !metadata.HasActivityFields() {
		return record, &codexsession.ParseError{Kind: codexsession.ErrorInvalidField}
	}
	timestamp, err := time.Parse(time.RFC3339Nano, metadata.Timestamp)
	if err != nil {
		return record, &codexsession.ParseError{Kind: codexsession.ErrorInvalidField}
	}
	worktreeID, project, ok := WorktreeFromCWD(metadata.CWD, file.home)
	if !ok {
		return record, nil
	}
	record.Valid = true
	record.WorktreeID = worktreeID
	record.Project = project
	record.Timestamp = timestamp
	return record, nil
}

func WorktreeFromCWD(cwd, home string) (string, string, bool) {
	home = canonicalPath(home)
	if runtime.GOOS == "darwin" {
		// Derive the workspace root from the indexed source, without repeatedly
		// probing the Orca layout for every session record and member lookup.
		userHome := home
		for range 5 {
			userHome = filepath.Dir(userHome)
		}
		if home == filepath.Join(userHome, "Library", "Application Support", "orca", "codex-runtime-home", "home") {
			root := canonicalPath(filepath.Join(userHome, "orca", "workspaces"))
			rel, err := filepath.Rel(root, canonicalPath(cwd))
			parts := pathParts(rel)
			if err == nil && len(parts) >= 2 && parts[0] != ".." && !filepath.IsAbs(rel) {
				// Nested CWDs lock the whole <repo>/<worktree> owner.
				return filepath.Join(parts[0], parts[1]), parts[0], true
			}
		}
	}
	rel, err := filepath.Rel(home, canonicalPath(cwd))
	if err != nil {
		return "", "", false
	}
	parts := pathParts(rel)
	if len(parts) < 2 || !isCodexActivityWorktreeRoot(parts[0]) {
		return "", "", false
	}
	project := parts[1]
	if len(parts) > 2 {
		project = parts[2]
	}
	return parts[1], project, true
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
