package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

// ClaudeProjectAdapter discovers Claude Code project-store entries and
// classifies them from the working directory recorded in session metadata.
type ClaudeProjectAdapter struct{}

func (a *ClaudeProjectAdapter) Name() types.Tool {
	return types.ToolClaude
}

func (a *ClaudeProjectAdapter) Category() types.Category {
	return types.CategoryAgentState
}

func (a *ClaudeProjectAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	base, err := agentStateStoreRootFor(filepath.Join(".claude", "projects"))
	if err != nil {
		return nil, err
	}
	if !pathUnderRoots(base, roots) {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var results []types.DebrisInfo
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath := filepath.Join(base, entry.Name())
		classification, reason, project, err := classifyClaudeProjectEntry(ctx, entryPath)
		if err != nil {
			return nil, err
		}
		results = append(results, types.DebrisInfo{
			Tool:           types.ToolClaude,
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

// ClassifyClaudeProjectEntry re-derives the cleanup-driving classification
// from the current contents of a Claude project-store entry.
func ClassifyClaudeProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, error) {
	classification, _, _, err := classifyClaudeProjectEntry(ctx, entryPath)
	return classification, err
}

func (a *ClaudeProjectAdapter) RevalidateAgentState(ctx context.Context, entryPath string) (types.EntryClass, error) {
	return ClassifyClaudeProjectEntry(ctx, entryPath)
}

func classifyClaudeProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, string, string, error) {
	return classifyRecordedCWDEntry(ctx, entryPath, recordedCWDsFromClaudeProject)
}

func recordedCWDsFromClaudeProject(ctx context.Context, entryPath string) (recordedCWDEvidence, error) {
	var evidence recordedCWDEvidence
	entries, err := os.ReadDir(entryPath)
	if err != nil {
		evidence.unverifiableFiles = append(evidence.unverifiableFiles, filepath.Base(entryPath))
		// Deliberately fail closed: unreadable project metadata is unverifiable
		// evidence for this entry, not a reason to abort the entire scan.
		return evidence, nil //nolint:nilerr
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return recordedCWDEvidence{}, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		readResult, err := readRecordedCWDs(ctx, filepath.Join(entryPath, entry.Name()))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return recordedCWDEvidence{}, err
			}
			evidence.unverifiableFiles = append(evidence.unverifiableFiles, entry.Name())
		}
		if readResult.unverifiableRecords > 0 {
			evidence.unverifiableRecords += readResult.unverifiableRecords
			if evidence.firstUnverifiableRecord == "" {
				evidence.firstUnverifiableRecord = fmt.Sprintf("%s:%d",
					entry.Name(), readResult.firstUnverifiableLine)
			}
		}
		for _, cwd := range readResult.cwds {
			if !seen[cwd] {
				seen[cwd] = true
				evidence.cwds = append(evidence.cwds, cwd)
			}
		}
	}
	return evidence, nil
}
