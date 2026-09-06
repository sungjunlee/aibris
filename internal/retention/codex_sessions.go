package retention

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/codexsession"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	errStoreRootSymlink      = errors.New("store root is a symlink")
	errStoreRootNotDirectory = errors.New("store root is not a directory")
	errStoreRootUnreadable   = errors.New("store root permission denied or unreadable")
	errStoreReadFailed       = errors.New("permission denied or unreadable store subtree")
)

// CodexSessionsProvider inventories regular rollout files under the exact
// sessions root of the resolved Codex home ($CODEX_HOME, or ~/.codex when
// unset) as read-only UTC-month aggregates. The inventory is
// protected content, not debris: it never feeds totals, caching, or any
// cleanup authorization, and no member path or transcript content is exposed.
//
// Traversal uses filepath.WalkDir, which does not follow directory symlinks;
// symlinked or non-regular leaves and unrecognized layout entries are skipped
// silently so the inventory reports only recognized regular rollout files.
type CodexSessionsProvider struct{}

func NewCodexSessionsProvider() *CodexSessionsProvider {
	return &CodexSessionsProvider{}
}

func (p *CodexSessionsProvider) Name() types.RetentionStoreID {
	return types.RetentionStoreCodexSessions
}

func (p *CodexSessionsProvider) Scan(
	ctx context.Context,
	opts types.ScanOptions,
) (types.RetentionProjection, error) {
	projection := emptyProjection()
	if err := ctx.Err(); err != nil {
		return projection, err
	}

	root, err := codexSessionsRoot()
	if err != nil {
		addProviderError(&projection, "resolving store root", err)
		return projection, nil
	}
	if !storeSelected(root, rootsCoveringCodexHome(opts.Roots)) {
		return projection, nil
	}

	info, err := os.Lstat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return projection, nil
		}
		// Deliberately path-free: diagnostics must never carry the store root
		// or any member path.
		addProviderError(&projection, "reading store root", errStoreRootUnreadable)
		return projection, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		addProviderError(&projection, "reading store root", errStoreRootSymlink)
		return projection, nil
	}
	if !info.IsDir() {
		addProviderError(&projection, "reading store root", errStoreRootNotDirectory)
		return projection, nil
	}

	state := newInventoryState(root)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			// Deliberately path-free: member and subtree paths must never
			// appear in the projection or its diagnostics.
			state.fail("reading store", errStoreReadFailed)
			return nil
		}
		return state.visit(ctx, path, entry)
	})
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return emptyProjection(), err
	default:
		// Unreachable today: the WalkDir callback only returns nil,
		// fs.SkipDir, or ctx.Err() (handled above). Kept path-free so a
		// future callback change can never surface a raw path-bearing
		// error in diagnostics.
		state.fail("walking store", errStoreReadFailed)
	}

	projection.Buckets = state.result()
	if state.partial {
		projection.Partial = true
		projection.ProviderErrors = append(projection.ProviderErrors, state.errs...)
	}
	return projection, nil
}

type inventoryState struct {
	root    string
	buckets map[string]*types.RetentionBucket
	errs    []types.RetentionProviderError
	partial bool
}

func newInventoryState(root string) *inventoryState {
	return &inventoryState{
		root:    root,
		buckets: make(map[string]*types.RetentionBucket),
	}
}

// visit classifies one walked entry against the fixed producer layout
// year/month/day/rollout-*.jsonl. Non-conforming directories are pruned so
// traversal stays bounded to the store root.
func (s *inventoryState) visit(ctx context.Context, path string, entry fs.DirEntry) error {
	relative, err := filepath.Rel(s.root, path)
	if err != nil || relative == "." {
		return nil
	}
	parts := strings.Split(relative, string(filepath.Separator))

	switch len(parts) {
	case 1:
		if entry.IsDir() && validYear(parts[0]) {
			return nil
		}
		return fs.SkipDir
	case 2:
		if entry.IsDir() && validMonth(parts[1]) {
			return nil
		}
		return fs.SkipDir
	case 3:
		if entry.IsDir() && validDay(parts[0], parts[1], parts[2]) {
			return nil
		}
		return fs.SkipDir
	case 4:
		if !entry.Type().IsRegular() || !isRolloutName(parts[3]) {
			return nil
		}
		return s.addUnit(ctx, path, entry)
	default:
		return fs.SkipDir
	}
}

// addUnit counts one recognized regular rollout leaf in its UTC-month bucket
// and, when the first metadata record proves a supported producer and an
// absent recorded cwd, contributes to the bucket's orphan aggregate.
func (s *inventoryState) addUnit(
	ctx context.Context,
	path string,
	entry fs.DirEntry,
) error {
	info, err := entry.Info()
	if err != nil {
		// Deliberately path-free: a concurrent-removal lstat failure must not
		// surface the leaf path.
		s.fail("reading session leaf", errStoreReadFailed)
		return nil
	}
	bucket := s.ensureBucket(bucketFromModTime(info.ModTime()))
	bucket.UnitCount++
	bucket.MemberCount++
	bucket.ApparentBytes += info.Size()

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			// Deliberately path-free: the raw open error embeds the member path.
			s.fail("reading session metadata", errStoreReadFailed)
		}
		return nil
	}
	defer func() { _ = file.Close() }()

	metadata, err := codexsession.ReadFirstMetadataFrom(ctx, file)
	if err != nil || !classifiableMetadata(metadata) {
		return nil
	}
	classification, classifyErr := adapter.ClassifyRecordedCWDs(ctx, []string{metadata.CWD}, true)
	if classifyErr != nil || classification != types.EntryClassOrphaned {
		return nil
	}
	bucket.OrphanedCount++
	bucket.OrphanedBytes += info.Size()
	return nil
}

func (s *inventoryState) ensureBucket(bucketID string) *types.RetentionBucket {
	if bucketID == "" {
		bucketID = retentionUnknownBucket
	}
	if existing := s.buckets[bucketID]; existing != nil {
		return existing
	}
	bucket := &types.RetentionBucket{
		StoreID:  types.RetentionStoreCodexSessions,
		BucketID: bucketID,
	}
	s.buckets[bucketID] = bucket
	return bucket
}

func (s *inventoryState) fail(stage string, err error) {
	s.partial = true
	s.errs = append(s.errs, types.RetentionProviderError{
		StoreID: types.RetentionStoreCodexSessions,
		Message: stage + ": " + err.Error(),
	})
}

func (s *inventoryState) result() []types.RetentionBucket {
	buckets := make([]types.RetentionBucket, 0, len(s.buckets))
	for _, bucket := range s.buckets {
		buckets = append(buckets, *bucket)
	}
	sort.Slice(buckets, func(i, j int) bool {
		return buckets[i].BucketID < buckets[j].BucketID
	})
	return buckets
}

func emptyProjection() types.RetentionProjection {
	return types.RetentionProjection{
		Buckets:        []types.RetentionBucket{},
		ProviderErrors: []types.RetentionProviderError{},
	}
}

func addProviderError(projection *types.RetentionProjection, stage string, err error) {
	projection.Partial = true
	projection.ProviderErrors = append(projection.ProviderErrors, types.RetentionProviderError{
		StoreID: types.RetentionStoreCodexSessions,
		Message: stage + ": " + err.Error(),
	})
}
