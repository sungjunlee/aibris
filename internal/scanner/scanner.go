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
		result.Worktrees = append(result.Worktrees, worktrees...)
	}
	if cancelErr != nil {
		return nil, cancelErr
	}
	sort.Slice(result.ProviderErrors, func(i, j int) bool {
		return result.ProviderErrors[i].Tool < result.ProviderErrors[j].Tool
	})

	result.Worktrees = requireTempDirOwnership(ctx, result.Worktrees, roots)

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

	result.Retention = scanRetention(ctx, opts, s.RetentionProviders)
	return result, nil
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
	if resolved != home && !isWithin(home, resolved) && !isResolvedSystemTempDir(resolved) {
		return "", fmt.Errorf("scan root %q must be under %s", raw, home)
	}
	return resolved, nil
}

// isResolvedSystemTempDir reports whether resolved is the system temp dir
// after the same symlink cleanup applied to every other root. The resolved
// system temp dir is the only root permitted outside the home tree, and only
// when explicitly supplied as a root: default roots never include it.
func isResolvedSystemTempDir(resolved string) bool {
	tempDir, err := resolveExistingPath(os.TempDir())
	if err != nil {
		return false
	}
	return resolved == tempDir
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

// optedInSystemTempRoot returns the normalized scan root that is the
// resolved system temp dir, or "" when the temp dir was not explicitly rooted
// or already sits inside the home tree. The ownership gate applies only to
// this explicit opt-in, so default scans are never affected.
func optedInSystemTempRoot(roots []string) string {
	tempDir, err := resolveExistingPath(os.TempDir())
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	resolvedHome, err := resolveExistingPath(home)
	if err != nil {
		return ""
	}
	if tempDir == resolvedHome || isWithin(resolvedHome, tempDir) {
		return ""
	}
	for _, root := range roots {
		if root == tempDir {
			return root
		}
	}
	return ""
}

// requireTempDirOwnership enforces per-unit ownership proof for debris found
// under an explicitly rooted system temp dir: the current user must own the
// path, and an agent-state store must record a cwd referencing it. Unproven
// units are dropped; proven units carry the owning-agent signal. Items
// outside the temp root pass through untouched.
func requireTempDirOwnership(ctx context.Context, items []types.DebrisInfo, roots []string) []types.DebrisInfo {
	tempRoot := optedInSystemTempRoot(roots)
	if tempRoot == "" {
		return items
	}
	// Fail closed: an unreadable owner index proves nothing, so no temp unit
	// surfaces.
	owners, err := adapter.RecordedCWDOwners(ctx)
	if err != nil {
		owners = nil
	}
	gated := items[:0]
	for _, item := range items {
		if item.Path != tempRoot && !isWithin(tempRoot, item.Path) {
			gated = append(gated, item)
			continue
		}
		if !pathOwnedByCurrentUser(item.Path) {
			continue
		}
		cwd, tool, ok := owningRecordedCWD(item.Path, owners)
		if !ok {
			continue
		}
		if item.Source == "" {
			item.Source = string(tool)
		}
		if item.Project == "" {
			item.Project = filepath.Base(filepath.Clean(cwd))
		}
		item.Reason = fmt.Sprintf("recorded cwd %s references this path (%s)", cwd, tool)
		gated = append(gated, item)
	}
	return gated
}

// owningRecordedCWD finds the recorded cwd that references path: an exact
// match, a cwd inside the unit, or a unit inside the cwd. Both sides resolve
// symlinks when they exist so /tmp-style aliases still match.
func owningRecordedCWD(path string, owners map[string]types.Tool) (string, types.Tool, bool) {
	unit := resolveForOwnershipMatch(path)
	cwds := make([]string, 0, len(owners))
	for cwd := range owners {
		cwds = append(cwds, cwd)
	}
	sort.Strings(cwds)
	for _, cwd := range cwds {
		resolved := resolveForOwnershipMatch(cwd)
		if resolved == unit || isWithin(resolved, unit) || isWithin(unit, resolved) {
			return cwd, owners[cwd], true
		}
	}
	return "", "", false
}

func resolveForOwnershipMatch(path string) string {
	if resolved, err := resolveExistingPath(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
