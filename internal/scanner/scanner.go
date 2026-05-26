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

var defaultProviders = []adapter.DebrisProvider{
	&adapter.CodexAdapter{},
	&adapter.ClaudeAdapter{},
	&adapter.NodeModulesAdapter{},
	&adapter.BuildCacheAdapter{},
	&adapter.PipCacheAdapter{},
	&adapter.CursorAdapter{},
	&adapter.AILogsAdapter{},
	&adapter.WindsurfAdapter{},
}

var DefaultScanner = New(defaultProviders)

type Scanner struct {
	Providers []adapter.DebrisProvider
}

func New(providers []adapter.DebrisProvider) *Scanner {
	return &Scanner{Providers: providers}
}

func Scan(ctx context.Context) (*types.ScanResult, error) {
	return DefaultScanner.Scan(ctx)
}

func (s *Scanner) Scan(ctx context.Context) (*types.ScanResult, error) {
	result := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}
	catByTool := make(map[types.Tool]types.Category)
	for _, p := range s.Providers {
		catByTool[p.Name()] = p.Category()
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
		cat := w.Category
		if cat == "" {
			cat = catByTool[w.Tool]
		}
		s := result.ByCategory[cat]
		s.Count++
		s.Size += w.Size
		result.ByCategory[cat] = s

		t := result.ByTool[w.Tool]
		t.Count++
		t.Size += w.Size
		result.ByTool[w.Tool] = t
	}

	sort.Slice(result.Worktrees, func(i, j int) bool {
		return result.Worktrees[i].Size > result.Worktrees[j].Size
	})

	return result, nil
}
