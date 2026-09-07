package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type CursorAdapter struct{}

func (a *CursorAdapter) Name() types.Tool {
	return types.ToolCursor
}

func (a *CursorAdapter) Category() types.Category {
	return types.CategoryAgentState
}

func (a *CursorAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	base, err := agentStateStoreRootFor(filepath.Join(".cursor", "projects"))
	if err != nil {
		return nil, err
	}
	if !pathUnderRoots(base, roots) {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []types.DebrisInfo
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath := filepath.Join(base, entry.Name())
		classification, reason, project, err := classifyCursorProjectEntry(ctx, entryPath)
		if err != nil {
			return nil, err
		}
		results = append(results, types.DebrisInfo{
			Tool:           types.ToolCursor,
			Category:       types.CategoryAgentState,
			ID:             entry.Name(),
			Project:        project,
			Path:           entryPath,
			ModTime:        agentStoreActivityModTime(ctx, entryPath, info.ModTime()),
			PathModTime:    info.ModTime(),
			Classification: classification,
			Reason:         reason,
		})
	}
	sizePaths := make([]string, 0, len(results))
	for _, result := range results {
		sizePaths = append(sizePaths, result.Path)
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

// ClassifyCursorProjectEntry re-derives the cleanup-driving classification
// from the current contents of a Cursor project-store entry.
func ClassifyCursorProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, error) {
	classification, _, _, err := classifyCursorProjectEntry(ctx, entryPath)
	return classification, err
}

func (a *CursorAdapter) RevalidateAgentState(ctx context.Context, entryPath string) (types.EntryClass, error) {
	return ClassifyCursorProjectEntry(ctx, entryPath)
}

func classifyCursorProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, string, string, error) {
	return classifyRecordedCWDEntry(ctx, entryPath, recordedCWDFromCursorProject)
}
