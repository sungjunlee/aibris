package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

type AILogsAdapter struct{}

func (a *AILogsAdapter) Name() types.Tool {
	return types.ToolAILogs
}

func (a *AILogsAdapter) Category() types.Category {
	return types.CategoryAILogs
}

// Scan reports AI log stores. Codex candidates follow the resolved Codex
// home ($CODEX_HOME, plus the extra homes listed in $AIBRIS_CODEX_HOMES)
// instead of assuming ~/.codex. Default $HOME scans still cover a Codex home
// outside $HOME; explicit --root is a hard boundary and is not widened.
func (a *AILogsAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}
	roots, err = applyCodexHomeScanRoots(roots)
	if err != nil {
		return nil, err
	}
	codexHomes, err := codexhome.Homes()
	if err != nil {
		return nil, err
	}

	var results []types.DebrisInfo

	candidates := codexLogCandidates(codexHomes)
	candidates = append(candidates,
		aiLogsCandidate{id: "claude-command-log", path: filepath.Join(home, ".claude", "command-audit.log")},
		aiLogsCandidate{id: "claude-file-history", path: filepath.Join(home, ".claude", "file-history")},
	)

	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !pathUnderRoots(c.path, roots) {
			continue
		}
		info, err := os.Stat(c.path)
		if err != nil {
			continue
		}
		results = append(results, types.DebrisInfo{
			Tool:     types.ToolAILogs,
			Category: types.CategoryAILogs,
			ID:       c.id,
			Path:     c.path,
			Size:     estimateDirSize(ctx, c.path),
			ModTime:  info.ModTime(),
		})
	}

	return results, nil
}

type aiLogsCandidate struct {
	id   string
	path string
}

// codexLogCandidates returns one codex-logs/codex-archived candidate pair per
// Codex home. The primary home keeps the historical IDs; each extra home is
// reported separately with a numeric suffix so rows stay distinct and
// attributable by path.
func codexLogCandidates(codexHomes []string) []aiLogsCandidate {
	candidates := make([]aiLogsCandidate, 0, 2*len(codexHomes))
	for i, codexHome := range codexHomes {
		suffix := ""
		if i > 0 {
			suffix = "-" + strconv.Itoa(i+1)
		}
		candidates = append(candidates,
			aiLogsCandidate{id: "codex-logs" + suffix, path: filepath.Join(codexHome, "logs_2.sqlite")},
			aiLogsCandidate{id: "codex-archived" + suffix, path: filepath.Join(codexHome, "archived_sessions")},
		)
	}
	return candidates
}
