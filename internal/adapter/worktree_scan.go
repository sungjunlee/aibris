package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// This file is the scanEntry/root-walk cluster for worktree Scan: walking a
// discovered container, classifying one physical owner, and filling DebrisInfo
// identity fields. Scan orchestration and the container registry stay in
// worktree.go.

func (a *WorktreeAdapter) scanWorktreeRootWithSource(ctx context.Context, root worktreeRoot, visited map[string]bool) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(root.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading worktree container %q: %w", root.path, err)
	}

	var results []types.DebrisInfo
	var sizePaths []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(root.path, entry.Name())
		canonicalEntry := canonicalExistingPath(entryPath)
		if visited[canonicalEntry] {
			continue
		}
		source := root.source
		if source == "" {
			source = detectWorktreeSource(entryPath)
		}
		items, err := a.scanEntry(ctx, canonicalEntry, source, root.memberDepth)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			continue
		}
		visited[canonicalEntry] = true
		sizePaths = append(sizePaths, canonicalEntry)
		results = append(results, items...)
	}
	sizes := estimateDirSizes(ctx, sizePaths)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Size = sizes[results[i].Path]
	}
	return results, nil
}

// scanEntry scans one physical worktree-container unit. Direct-style tools
// place .git in the unit itself; nested-style tools place it in one or more
// immediate project directories. Any invalid nested marker protects the whole
// outer unit, so no valid sibling can become a separate cleanup target.
func (a *WorktreeAdapter) scanEntry(ctx context.Context, entryPath, source string, memberDepth int) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entryInfo, err := os.Stat(entryPath)
	if err != nil {
		return nil, fmt.Errorf("inspecting worktree unit %q: %w", entryPath, err)
	}

	direct, err := inspectWorktreeMarker(ctx, filepath.Join(entryPath, ".git"))
	if err != nil {
		return nil, err
	}
	switch direct.state {
	case worktreeMarkerValid:
		item := newWorktreeItem(entryPath, entryPath, source, direct.status, "", entryInfo.ModTime())
		item.StrippableBytes, item.StrippablePaths = a.strippableSubtrees(ctx, entryPath, direct.status)
		return []types.DebrisInfo{item}, nil
	case worktreeMarkerInvalid:
		return []types.DebrisInfo{
			newWorktreeItem(entryPath, entryPath, source, types.WorktreePlain, direct.reason, entryInfo.ModTime()),
		}, nil
	}

	entries, err := os.ReadDir(entryPath)
	if err != nil {
		return nil, fmt.Errorf("reading worktree unit %q: %w", entryPath, err)
	}

	var valid []types.DebrisInfo
	var invalidReasons []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		worktreePath := filepath.Join(entryPath, entry.Name())
		inspection, err := inspectWorktreeMarker(ctx, filepath.Join(worktreePath, ".git"))
		if err != nil {
			return nil, err
		}
		switch inspection.state {
		case worktreeMarkerValid:
			item := newWorktreeItem(
				entryPath,
				worktreePath,
				source,
				inspection.status,
				"",
				entryInfo.ModTime(),
			)
			item.StrippableBytes, item.StrippablePaths = a.strippableSubtrees(ctx, worktreePath, inspection.status)
			valid = append(valid, item)
		case worktreeMarkerMissing:
			nestedValid, nestedInvalid, ignore, err := a.inspectMissingMember(
				ctx, entryPath, worktreePath, entry.Name(), source, entryInfo.ModTime(), memberDepth,
			)
			if err != nil {
				return nil, err
			}
			if ignore {
				continue
			}
			valid = append(valid, nestedValid...)
			invalidReasons = append(invalidReasons, nestedInvalid...)
		case worktreeMarkerInvalid:
			invalidReasons = append(invalidReasons, fmt.Sprintf("%s: %s", entry.Name(), inspection.reason))
		}
	}

	if len(invalidReasons) > 0 {
		sort.Strings(invalidReasons)
		return []types.DebrisInfo{
			newWorktreeItem(
				entryPath,
				entryPath,
				source,
				types.WorktreePlain,
				"invalid linked worktree metadata: "+strings.Join(invalidReasons, "; "),
				entryInfo.ModTime(),
			),
		}, nil
	}
	if len(valid) == 0 {
		return []types.DebrisInfo{
			newWorktreeItem(entryPath, entryPath, source, types.WorktreePlain, noWorktreeMetadataReason, entryInfo.ModTime()),
		}, nil
	}

	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Project != valid[j].Project {
			return valid[i].Project < valid[j].Project
		}
		return valid[i].Status < valid[j].Status
	})
	return valid, nil
}

func newWorktreeItem(
	entryPath,
	worktreePath,
	source string,
	status types.WorktreeStatus,
	reason string,
	modTime time.Time,
) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     detectWorktreeTool(source),
		Category: types.CategoryWorktree,
		ID:       filepath.Base(entryPath),
		Project:  detectWorktreeProject(entryPath, worktreePath, source),
		Source:   source,
		Path:     entryPath,
		ModTime:  modTime,
		Status:   status,
		Reason:   reason,
	}
}

func detectWorktreeSource(entryPath string) string {
	worktreeRoot := filepath.Dir(entryPath)
	owner := filepath.Base(filepath.Dir(worktreeRoot))
	if isHiddenDir(owner) {
		return owner
	}
	return projectLocalSource
}

func detectWorktreeTool(source string) types.Tool {
	switch source {
	case ".codex":
		return types.ToolCodex
	case ".claude":
		return types.ToolClaude
	default:
		return types.ToolUnknown
	}
}

func detectWorktreeProject(entryPath, worktreePath, source string) string {
	if source == ".claude" {
		worktreeRoot := filepath.Dir(entryPath)
		ownerDir := filepath.Dir(worktreeRoot)
		if filepath.Base(ownerDir) == ".claude" {
			return filepath.Base(filepath.Dir(ownerDir))
		}
	}
	if worktreePath != entryPath {
		return filepath.Base(worktreePath)
	}
	return filepath.Base(entryPath)
}
