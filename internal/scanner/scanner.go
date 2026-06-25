package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

var defaultProviders = []adapter.DebrisProvider{
	&adapter.NodeModulesAdapter{},
	&adapter.BuildCacheAdapter{},
	&adapter.PipCacheAdapter{},
	&adapter.CursorAdapter{},
	&adapter.AILogsAdapter{},
	&adapter.WindsurfAdapter{},
	adapter.NewWorktreeAdapter(),
}

var DefaultScanner = New(defaultProviders)

const maxParallelProviders = 2

type Scanner struct {
	Providers   []adapter.DebrisProvider
	ErrorWriter io.Writer
}

func (s *Scanner) errw() io.Writer {
	if s.ErrorWriter != nil {
		return s.ErrorWriter
	}
	return os.Stderr
}

func New(providers []adapter.DebrisProvider) *Scanner {
	return &Scanner{Providers: providers}
}

func Scan(ctx context.Context) (*types.ScanResult, error) {
	return DefaultScanner.Scan(ctx)
}

func ScanWithOptions(ctx context.Context, opts types.ScanOptions) (*types.ScanResult, error) {
	return DefaultScanner.ScanWithOptions(ctx, opts)
}

func (s *Scanner) Scan(ctx context.Context) (*types.ScanResult, error) {
	opts, err := DefaultScanOptions()
	if err != nil {
		return nil, err
	}
	return s.ScanWithOptions(ctx, opts)
}

func (s *Scanner) ScanWithOptions(ctx context.Context, opts types.ScanOptions) (*types.ScanResult, error) {
	roots, err := NormalizeRoots(opts.Roots)
	if err != nil {
		return nil, err
	}
	opts.Roots = roots

	result := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}
	catByTool := make(map[types.Tool]types.Category)
	for _, p := range s.Providers {
		catByTool[p.Name()] = p.Category()
	}

	// Fast path: return immediately if context is already cancelled.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan providerScanResult, len(s.Providers))
	startGate := make(chan struct{})
	sem := make(chan struct{}, maxParallelProviders)
	var wg sync.WaitGroup

	for _, p := range s.Providers {
		emitProgress(opts.OnProgress, types.ScanProgressEvent{
			State: types.ScanProgressStart,
			Tool:  p.Name(),
		})
		wg.Add(1)
		go func(p adapter.DebrisProvider) {
			defer wg.Done()
			select {
			case <-scanCtx.Done():
				results <- providerScanResult{provider: p, err: scanCtx.Err()}
				return
			case <-startGate:
			}
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-scanCtx.Done():
				results <- providerScanResult{provider: p, err: scanCtx.Err()}
				return
			}
			items, err := p.Scan(scanCtx, opts)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				cancel()
			}
			results <- providerScanResult{provider: p, items: items, err: err}
		}(p)
	}
	close(startGate)

	go func() {
		wg.Wait()
		close(results)
	}()

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
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				cancelErr = err
			}
			continue
		}
		emitProgress(opts.OnProgress, types.ScanProgressEvent{
			State: types.ScanProgressDone,
			Tool:  p.Name(),
			Count: len(worktrees),
			Size:  totalSize(worktrees),
		})
		result.Worktrees = append(result.Worktrees, worktrees...)
	}
	if cancelErr != nil {
		return nil, cancelErr
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

type providerScanResult struct {
	provider adapter.DebrisProvider
	items    []types.DebrisInfo
	err      error
}

func emitProgress(fn func(types.ScanProgressEvent), event types.ScanProgressEvent) {
	if fn != nil {
		fn(event)
	}
}

func totalSize(items []types.DebrisInfo) int64 {
	var size int64
	for _, item := range items {
		size += item.Size
	}
	return size
}

func DefaultScanOptions() (types.ScanOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return types.ScanOptions{}, err
	}
	roots, err := NormalizeRoots([]string{home})
	if err != nil {
		return types.ScanOptions{}, err
	}
	return types.ScanOptions{Roots: roots}, nil
}

func NormalizeRoots(rawRoots []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	resolvedHome, err := resolveExistingPath(home)
	if err != nil {
		return nil, fmt.Errorf("resolving home: %w", err)
	}

	if len(rawRoots) == 0 {
		rawRoots = []string{resolvedHome}
	}

	seen := make(map[string]bool)
	var roots []string
	for _, raw := range rawRoots {
		root, err := normalizeRoot(raw, resolvedHome)
		if err != nil {
			return nil, err
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}

	sort.Strings(roots)
	var deduped []string
	for _, root := range roots {
		nested := false
		for _, parent := range deduped {
			if root == parent || isWithin(parent, root) {
				nested = true
				break
			}
		}
		if !nested {
			deduped = append(deduped, root)
		}
	}
	return deduped, nil
}

func normalizeRoot(raw, home string) (string, error) {
	root := strings.TrimSpace(raw)
	if root == "" {
		return "", fmt.Errorf("scan root cannot be empty")
	}
	if root == "~" {
		root = home
	} else if strings.HasPrefix(root, "~/") {
		root = filepath.Join(home, strings.TrimPrefix(root, "~/"))
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("scan root %q must be absolute or start with ~", raw)
	}
	resolved, err := resolveExistingPath(root)
	if err != nil {
		return "", fmt.Errorf("resolving scan root %q: %w", raw, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("reading scan root %q: %w", raw, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("scan root %q is not a directory", raw)
	}
	if resolved != home && !isWithin(home, resolved) {
		return "", fmt.Errorf("scan root %q must be under %s", raw, home)
	}
	return resolved, nil
}

func resolveExistingPath(path string) (string, error) {
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
