package adapter

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

// worktreeSource defines a scan target for AI tool worktrees.
type worktreeSource struct {
	tool    types.Tool
	pattern string // glob suffix relative to home, e.g. ".codex/worktrees/*"
}

// knownSources are well-known AI tool patterns that get correct tool attribution.
// These are matched first; their paths are tracked to avoid duplicates from the
// generic catch-all pattern.
var knownSources = []worktreeSource{
	{tool: types.ToolCodex, pattern: ".codex/worktrees/*"},
	{tool: types.ToolClaude, pattern: "*/.claude/worktrees/*"},
}

// genericPattern catches any <dir>/worktree*/* under home.
// This discovers worktrees from any tool — relay, project-local, future tools —
// without hardcoding specific paths. All matches get ToolUnknown.
const genericPattern = "*/worktree*/*"

// WorktreeAdapter discovers git worktrees created by AI coding tools
// and reports their health status (active vs orphaned).
type WorktreeAdapter struct{}

func NewWorktreeAdapter() *WorktreeAdapter {
	return &WorktreeAdapter{}
}

func (a *WorktreeAdapter) Name() types.Tool {
	return types.ToolCodex
}

func (a *WorktreeAdapter) Category() types.Category {
	return types.CategoryWorktree
}

func (a *WorktreeAdapter) Scan(ctx context.Context) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// visited tracks entry paths already reported by known patterns,
	// to prevent duplicates from the generic catch-all.
	visited := make(map[string]bool)
	var results []types.DebrisInfo

	// 1. Known tool patterns (specific, with correct tool attribution)
	for _, src := range knownSources {
		items, err := a.scanSource(ctx, home, src, visited)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}

	// 2. Generic catch-all: any */worktree*/* path not already reported
	generic := worktreeSource{tool: types.ToolUnknown, pattern: genericPattern}
	items, err := a.scanSource(ctx, home, generic, visited)
	if err != nil {
		return nil, err
	}
	results = append(results, items...)

	return results, nil
}

// scanSource applies a single source pattern and returns found worktrees.
// Already-visited entry paths are skipped.
func (a *WorktreeAdapter) scanSource(ctx context.Context, home string, src worktreeSource, visited map[string]bool) ([]types.DebrisInfo, error) {
	pattern := filepath.Join(home, src.pattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, nil
	}

	var results []types.DebrisInfo
	for _, match := range matches {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		info, err := os.Stat(match)
		if err != nil || !info.IsDir() {
			continue
		}

		// Skip if already reported by a more specific pattern.
		// Glob already returns absolute paths (pattern is absolute), so
		// match is fully qualified.
		if visited[match] {
			continue
		}
		visited[match] = true

		items := a.scanEntry(ctx, match, src)
		results = append(results, items...)
	}
	return results, nil
}

// scanEntry scans a single glob match directory for git worktrees.
//
// Claude places .git directly in the match directory; codex and relay
// nest .git inside a project subdirectory. This handles both patterns.
func (a *WorktreeAdapter) scanEntry(ctx context.Context, entryPath string, src worktreeSource) []types.DebrisInfo {
	// Pattern 1: .git directly inside the entry (claude)
	if item := a.checkWorktree(ctx, entryPath, entryPath, src); item != nil {
		return []types.DebrisInfo{*item}
	}

	// Pattern 2: .git inside a subdirectory (codex, relay, project-local)
	entries, err := os.ReadDir(entryPath)
	if err != nil {
		return nil
	}

	var results []types.DebrisInfo
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if !e.IsDir() {
			continue
		}
		subPath := filepath.Join(entryPath, e.Name())
		if item := a.checkWorktree(ctx, entryPath, subPath, src); item != nil {
			results = append(results, *item)
		}
	}
	return results
}

// checkWorktree checks if worktreePath contains a git worktree (.git file).
// Returns a DebrisInfo if found, nil otherwise.
func (a *WorktreeAdapter) checkWorktree(ctx context.Context, entryPath, worktreePath string, src worktreeSource) *types.DebrisInfo {
	gitFile := filepath.Join(worktreePath, ".git")
	gitInfo, err := os.Stat(gitFile)
	if err != nil || gitInfo.IsDir() {
		return nil
	}

	status := detectWorktreeStatus(gitFile)
	project := detectWorktreeProject(entryPath, worktreePath, src)

	entryInfo, err := os.Stat(entryPath)
	if err != nil {
		return nil
	}

	return &types.DebrisInfo{
		Tool:     src.tool,
		Category: types.CategoryWorktree,
		ID:       filepath.Base(entryPath),
		Project:  project,
		Path:     entryPath,
		Size:     estimateDirSize(ctx, entryPath),
		ModTime:  entryInfo.ModTime(),
		Status:   status,
	}
}

// detectWorktreeStatus reads the .git file and checks if the parent gitdir exists.
func detectWorktreeStatus(gitFilePath string) types.WorktreeStatus {
	gitdirPath := readGitDir(gitFilePath)
	if gitdirPath == "" {
		return types.WorktreePlain
	}
	if _, err := os.Stat(gitdirPath); os.IsNotExist(err) {
		return types.WorktreeOrphaned
	}
	return types.WorktreeActive
}

// readGitDir reads a .git worktree file and returns the gitdir path.
// Returns "" if the file can't be read or doesn't contain a valid gitdir line.
func readGitDir(gitFilePath string) string {
	f, err := os.Open(gitFilePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "gitdir: ") {
			return strings.TrimPrefix(line, "gitdir: ")
		}
	}
	return ""
}

// detectWorktreeProject determines the project name based on the source tool convention.
func detectWorktreeProject(entryPath, worktreePath string, src worktreeSource) string {
	switch src.tool {
	case types.ToolClaude:
		// claude: entry = ~/<project>/.claude/worktrees/<name>
		// project = "<project>"
		root := filepath.Dir(filepath.Dir(filepath.Dir(entryPath)))
		return filepath.Base(root)
	case types.ToolUnknown:
		// generic: use the worktree-bearing directory name.
		// For subdir-style (codex/relay-like) this is the project subdirectory;
		// for direct-style (claude-like) this is the worktree/session name.
		return filepath.Base(worktreePath)
	default:
		// codex: project = first non-hidden subdirectory name
		return detectProjectName(entryPath)
	}
}
