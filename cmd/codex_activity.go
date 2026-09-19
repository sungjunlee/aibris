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

// Session-file discovery, record parsing, and CWD worktree identity for the
// Codex activity index. Index loading stays in codex_activity.go. Cache I/O
// and aggregation live in codex_activity_cache.go.

type codexSessionFileInfo struct {
	path    string
	modTime time.Time
	size    int64
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
