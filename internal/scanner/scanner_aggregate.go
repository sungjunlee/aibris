package scanner

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sungjunlee/aibris/internal/types"
)

// aggregateProviderResults folds providerScanResult values into the inventory:
// progress, diagnostics, provider errors, ownership, user exclusions, totals,
// size order, and retention. Cancel/deadline errors abort before inventory
// post-processing.
func (s *Scanner) aggregateProviderResults(
	ctx context.Context,
	opts types.ScanOptions,
	roots []string,
	results <-chan providerScanResult,
) (*types.ScanResult, error) {
	result := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}
	catByTool := make(map[types.Tool]types.Category)
	for _, p := range s.Providers {
		catByTool[p.Name()] = p.Category()
	}

	var cancelErr error
	for providerResult := range results {
		p := providerResult.provider
		worktrees := providerResult.items
		err := providerResult.err
		if err != nil {
			emitProgress(opts.OnProgress, types.ScanProgressEvent{
				State: types.ScanProgressError,
				Tool:  p.Name(),
				Err:   err,
			})
			fmt.Fprintf(s.errw(), "scan:%s:%v\n", p.Name(), err)
			if opts.Diagnostics {
				result.Diagnostics = append(result.Diagnostics, types.ProviderDiagnostic{
					Tool:     p.Name(),
					State:    types.ScanProgressError,
					Duration: providerResult.duration,
					Err:      err.Error(),
				})
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				cancelErr = err
			} else {
				result.ProviderErrors = append(result.ProviderErrors, types.ScanProviderError{
					Tool:    p.Name(),
					Message: err.Error(),
				})
			}
			continue
		}
		emitProgress(opts.OnProgress, types.ScanProgressEvent{
			State: types.ScanProgressDone,
			Tool:  p.Name(),
			Count: len(worktrees),
			Size:  totalSize(worktrees),
		})
		if opts.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, types.ProviderDiagnostic{
				Tool:     p.Name(),
				State:    types.ScanProgressDone,
				Count:    len(worktrees),
				Bytes:    totalSize(worktrees),
				Duration: providerResult.duration,
			})
		}
		result.Worktrees = append(result.Worktrees, worktrees...)
	}
	if cancelErr != nil {
		return nil, cancelErr
	}
	sort.Slice(result.ProviderErrors, func(i, j int) bool {
		return result.ProviderErrors[i].Tool < result.ProviderErrors[j].Tool
	})
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Tool < result.Diagnostics[j].Tool
	})

	result.Worktrees = requireTempDirOwnership(ctx, result.Worktrees, roots)

	applyUserExclusions(result, opts)
	fillInventoryTotals(result, catByTool)

	sort.Slice(result.Worktrees, func(i, j int) bool {
		return result.Worktrees[i].Size > result.Worktrees[j].Size
	})

	result.Retention = scanRetention(ctx, opts, s.RetentionProviders)
	return result, nil
}
