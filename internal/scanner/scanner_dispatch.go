package scanner

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

const maxParallelProviders = 2

type providerScanResult struct {
	provider adapter.DebrisProvider
	items    []types.DebrisInfo
	err      error
	duration time.Duration
}

// dispatchProviders starts each provider Scan behind a shared start gate and
// semaphore, then returns the result channel. The caller owns scanCtx/cancel
// for the duration of aggregation.
func (s *Scanner) dispatchProviders(
	scanCtx context.Context,
	cancel context.CancelFunc,
	opts types.ScanOptions,
) <-chan providerScanResult {
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

	return results
}
