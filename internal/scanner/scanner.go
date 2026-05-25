package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

var providers = []adapter.WorktreeProvider{
	&adapter.CodexAdapter{},
	&adapter.ClaudeAdapter{},
}

func Scan(ctx context.Context) (*types.ScanResult, error) {
	result := &types.ScanResult{}
	for _, p := range providers {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		worktrees, err := p.Scan(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scan:%s:%v\n", p.Name(), err)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			continue
		}
		result.Worktrees = append(result.Worktrees, worktrees...)
	}

	result.TotalCount = len(result.Worktrees)
	for _, w := range result.Worktrees {
		result.TotalSize += w.Size
	}

	sort.Slice(result.Worktrees, func(i, j int) bool {
		return result.Worktrees[i].Size > result.Worktrees[j].Size
	})

	return result, nil
}
