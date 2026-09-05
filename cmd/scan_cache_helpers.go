package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

// lastScanSession is the shared cache session used by scan (write) and clean
// (reuse or live scan). Clean's scan-for-clean path loads through this type
// rather than a private fingerprint or schema.
type lastScanSession struct {
	roots    []string
	excludes []string
	selector string
	explicit bool
	progress bool
}

func (s lastScanSession) load(ctx context.Context) (*types.ScanResult, scanSource, error) {
	if result, source, ok := s.tryCached(); ok {
		if err := requireCompleteScan(result); err != nil {
			return nil, scanSource{}, err
		}
		return result, source, nil
	}
	return s.liveScan(ctx)
}

func (s lastScanSession) tryCached() (*types.ScanResult, scanSource, bool) {
	if reason, skip := s.excludedReason(); skip {
		printLastScanRescan(reason, s.progress)
		return nil, scanSource{}, false
	}
	readAt := time.Now()
	result, age, reason, ok := inspectLastScanCache(s.roots, s.selector, s.explicit)
	if ok && scanResultHasExclusions(result) {
		ok = false
		reason = "cached scan used exclusions"
	}
	if !ok || !claimLastScanSelector(s.selector) {
		printLastScanRescan(cachedScanMissReason(ok, reason), s.progress)
		return nil, scanSource{}, false
	}
	emitCachedExplicitRootWarning(s.roots, s.explicit)
	printLastScanReuse(age, s.progress)
	return result, scanSource{
		Kind:       scanSourceCached,
		Age:        age,
		ObservedAt: readAt.Add(-age),
	}, true
}

func (s lastScanSession) excludedReason() (string, bool) {
	if len(s.excludes) == 0 {
		return "", false
	}
	_, _, reason, ok := inspectLastScanCache(s.roots, s.selector, s.explicit)
	if !ok && reason == "" {
		return "", true
	}
	return "cleanup exclusions requested", true
}

func cachedScanMissReason(ok bool, reason string) string {
	if !ok {
		return reason
	}
	return "cache selector unavailable"
}

func printLastScanReuse(age time.Duration, show bool) {
	if !show {
		return
	}
	fmt.Printf("using last scan from %s ago\n", shortDurationString(age))
}

func printLastScanRescan(reason string, show bool) {
	if !show || reason == "" {
		return
	}
	fmt.Printf("scanning again: %s\n", reason)
}

func (s lastScanSession) liveScan(ctx context.Context) (*types.ScanResult, scanSource, error) {
	result, err := s.runLive(ctx)
	if err != nil {
		return nil, scanSource{}, err
	}
	if err := requireCompleteScan(result); err != nil {
		return nil, scanSource{}, err
	}
	writeLastScanCacheForSelector(s.roots, scanner.DefaultScanner.ProviderIdentity(), s.selector, s.explicit, result)
	return result, scanSource{Kind: scanSourceLive}, nil
}

func (s lastScanSession) runLive(ctx context.Context) (*types.ScanResult, error) {
	if s.progress {
		progress := newScanProgressPrinter(os.Stdout)
		result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{
			Roots:         s.roots,
			ExplicitRoots: s.explicit,
			Excludes:      s.excludes,
			OnProgress:    progress.Handle,
		})
		progress.Stop()
		return result, err
	}
	quietScanner := scanner.NewWithRetentionProviders(
		scanner.DefaultScanner.Providers,
		scanner.DefaultScanner.RetentionProviders,
	)
	quietScanner.ErrorWriter = io.Discard
	return quietScanner.ScanWithOptions(ctx, types.ScanOptions{
		Roots:         s.roots,
		ExplicitRoots: s.explicit,
		Excludes:      s.excludes,
	})
}

// scanResultHasExclusions reports whether a cached scan was produced with
// user exclusions applied, so a plain clean never reuses a filtered cache.
func scanResultHasExclusions(result *types.ScanResult) bool {
	return result.ExcludedByUser > 0 || len(result.ExcludedScopes) > 0 || len(result.RejectedExcludes) > 0
}
