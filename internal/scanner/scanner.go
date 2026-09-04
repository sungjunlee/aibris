package scanner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/types"
)

var defaultProviders = adapter.DefaultProviders()
var defaultRetentionProviders = retention.DefaultProviders()

var DefaultScanner = NewWithRetentionProviders(defaultProviders, defaultRetentionProviders)

const maxParallelProviders = 2

type Scanner struct {
	Providers          []adapter.DebrisProvider
	RetentionProviders []types.RetentionProvider
	ErrorWriter        io.Writer
	// Now is a clock seam for deterministic provider diagnostics; it
	// defaults to time.Now when nil.
	Now func() time.Time
}

func (s *Scanner) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scanner) emitUncoveredCodexHomeWarning(opts types.ScanOptions) error {
	warning, err := adapter.UncoveredCodexHomeWarning(opts)
	if err != nil || warning == "" {
		return err
	}
	line := "warning: " + warning + "\n"
	fmt.Fprint(s.errw(), line)
	// JSON clean discards provider errors on stderr; the --root diagnostic
	// still has to reach the operator, so re-emit when the writer is Discard.
	if s.ErrorWriter == io.Discard {
		fmt.Fprint(os.Stderr, line)
	}
	return nil
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

// ProviderIdentity identifies the concrete provider membership this scanner was
// built with. DefaultScanner reports the default registry identity; a scanner
// built with custom providers reports theirs, so cache producers stamp the
// identity of the exact provider set that produced the inventory.
func (s *Scanner) ProviderIdentity() string {
	return adapter.Identity(s.Providers)
}

// NewWithRetentionProviders builds a scanner with the optional read-only
// protected-content inventory. Retention providers never participate in
// debris totals or cleanup authorization.
func NewWithRetentionProviders(
	providers []adapter.DebrisProvider,
	retentionProviders []types.RetentionProvider,
) *Scanner {
	return &Scanner{
		Providers:          providers,
		RetentionProviders: retentionProviders,
	}
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
	if err := s.emitUncoveredCodexHomeWarning(opts); err != nil {
		return nil, err
	}

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
			start := s.now()
			items, err := p.Scan(scanCtx, opts)
			dur := s.now().Sub(start)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				cancel()
			}
			results <- providerScanResult{provider: p, items: items, err: err, duration: dur}
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

// applyUserExclusions removes discovered items covered by user exclusion
// patterns (--exclude flags, the per-user ignore file, and repo-local
// .aibris-ignore files under the scan roots). Exclusions affect discovery
// only; they never broaden deletion authority.
func applyUserExclusions(result *types.ScanResult, opts types.ScanOptions) {
	patterns := make([]exclude.Pattern, 0, len(opts.Excludes))
	for _, raw := range opts.Excludes {
		patterns = append(patterns, exclude.Pattern{Raw: raw, Source: types.ExcludeSourceFlag})
	}
	patterns = append(patterns, exclude.IgnoreFilePatterns(opts.Roots)...)
	if len(patterns) == 0 {
		return
	}
	matcher := exclude.New(patterns, opts.Roots)
	kept := result.Worktrees[:0]
	for _, item := range result.Worktrees {
		if matcher.Match(item.Path) {
			result.ExcludedByUser++
			continue
		}
		kept = append(kept, item)
	}
	result.Worktrees = kept
	result.ExcludedScopes = matcher.Scopes()
	result.RejectedExcludes = matcher.Rejected()
}

// scanRetention inventories protected-content stores after the debris scan.
// Store-local failures degrade the retention projection to partial without
// affecting debris results or cleanup authorization.
func scanRetention(
	ctx context.Context,
	opts types.ScanOptions,
	providers []types.RetentionProvider,
) types.RetentionProjection {
	projection := types.RetentionProjection{
		Buckets:        []types.RetentionBucket{},
		ProviderErrors: []types.RetentionProviderError{},
	}
	seen := make(map[string]bool)
	for _, provider := range providers {
		if err := ctx.Err(); err != nil {
			break
		}
		providerProjection, err := provider.Scan(ctx, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				projection.Partial = true
				break
			}
			projection.Partial = true
			projection.ProviderErrors = append(projection.ProviderErrors, types.RetentionProviderError{
				StoreID: provider.Name(),
				Message: "provider failure",
			})
			continue
		}
		if providerProjection.Partial || len(providerProjection.ProviderErrors) > 0 {
			projection.Partial = true
		}
		projection.ProviderErrors = append(projection.ProviderErrors, providerProjection.ProviderErrors...)
		for _, bucket := range providerProjection.Buckets {
			key := string(bucket.StoreID) + "\x00" + bucket.BucketID
			if seen[key] {
				projection.Partial = true
				projection.ProviderErrors = append(projection.ProviderErrors, types.RetentionProviderError{
					StoreID: provider.Name(),
					Message: "duplicate retention bucket",
				})
				continue
			}
			seen[key] = true
			projection.Buckets = append(projection.Buckets, bucket)
		}
	}
	sort.Slice(projection.Buckets, func(i, j int) bool {
		if projection.Buckets[i].StoreID == projection.Buckets[j].StoreID {
			return projection.Buckets[i].BucketID < projection.Buckets[j].BucketID
		}
		return projection.Buckets[i].StoreID < projection.Buckets[j].StoreID
	})
	sort.Slice(projection.ProviderErrors, func(i, j int) bool {
		if projection.ProviderErrors[i].StoreID == projection.ProviderErrors[j].StoreID {
			return projection.ProviderErrors[i].Message < projection.ProviderErrors[j].Message
		}
		return projection.ProviderErrors[i].StoreID < projection.ProviderErrors[j].StoreID
	})
	return projection
}

type providerScanResult struct {
	provider adapter.DebrisProvider
	items    []types.DebrisInfo
	err      error
	duration time.Duration
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
