package codexactivity

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

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	CacheSchemaVersion = 1
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
	return opts
}

func (i Index) ProjectHasSessionAfter(project string, ts time.Time) bool {
	if !i.Available || project == "" {
		return false
	}
	activity, ok := i.Projects[project]
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

// DefaultSessionRoots returns the Codex session roots under the
// resolved Codex home ($CODEX_HOME, or ~/.codex when unset).
func DefaultSessionRoots() ([]string, error) {
	codexHome, err := codexhome.Home()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(codexHome, "sessions"),
		filepath.Join(codexHome, "archived_sessions"),
	}, nil
}

// Session-file discovery, record parsing, and CWD worktree identity for the
// Codex activity index. Cache I/O and aggregation live in cache.go.

type sessionFileInfo struct {
	path    string
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

func appendSessionFileInfo(files []sessionFileInfo, seen map[string]bool, path string, info fs.FileInfo) []sessionFileInfo {
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

func readSessionFileRecord(file sessionFileInfo) (FileRecord, error) {
	record := FileRecord{
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
	worktreeID, project, ok := WorktreeFromCWD(meta.Payload.CWD)
	if !ok {
		return record, nil
	}

	record.Valid = true
	record.WorktreeID = worktreeID
	record.Project = project
	record.Timestamp = timestamp
	return record, nil
}

func WorktreeFromCWD(cwd string) (string, string, bool) {
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
