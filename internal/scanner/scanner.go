package scanner

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/types"
)

var defaultProviders = adapter.DefaultProviders()
var defaultRetentionProviders = retention.DefaultProviders()

var DefaultScanner = NewWithRetentionProviders(defaultProviders, defaultRetentionProviders)

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

	// Fast path: return immediately if context is already cancelled.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := s.dispatchProviders(scanCtx, cancel, opts)
	return s.aggregateProviderResults(ctx, opts, roots, results)
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
