package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	codexTmpDirName = "tmp"

	codexTmpReasonNoExclusion       = "producer-cooperative exclusion unavailable"
	codexTmpReasonIncompleteWriters = "writer registry does not cover every producer class"
	codexTmpReasonUnknownVersion    = "unknown producer version"
	codexTmpReasonUnknownLayout     = "unknown tmp layout"
	codexTmpReasonSymlinkChild      = "direct child is a symlink"
	codexTmpReasonUnknownEntry      = "unknown direct-child entry type"
	codexTmpReasonSymlinkRoot       = "tmp root is a symlink"
	codexTmpReasonNotDirectory      = "tmp root is not a directory"
	codexTmpReasonInventory         = "descendant inventory failed"
	codexTmpReasonUnknownSymlink    = "symlink target is unaccounted"
	codexTmpReasonFenceLost         = "exclusion token lost"
	codexTmpReasonSnapshotMismatch  = "unit snapshot mismatch"
	codexTmpReasonNotDirectChild    = "path is not a direct child of the tmp root"
	codexTmpReasonTmpRoot           = "tmp root is never a deletion unit"
)

var (
	_ DebrisProvider = (*CodexTmpAdapter)(nil)

	codexTmpRequiredWriters = []codexTmpWriterClass{
		codexTmpWriterGUI,
		codexTmpWriterCLI,
		codexTmpWriterApplyPatch,
		codexTmpWriterSupervisor,
	}
)

// CodexTmpAdapter evaluates regenerable residue under each Codex home's tmp
// directory. Production admits no layouts: no producer-documented versioned
// identity or all-writer fencing exists yet, so every direct child stays
// ineligible. The tmp root is never a deletion unit. This adapter is not
// registered in defaultProviders until a producer layout can be admitted.
type CodexTmpAdapter struct {
	version   string
	layouts   []codexTmpLayout
	exclusion *codexTmpCooperativeExclusion
}

func (a *CodexTmpAdapter) Name() types.Tool {
	return types.ToolCodex
}

func (a *CodexTmpAdapter) Category() types.Category {
	return types.CategoryOtherCache
}

// Scan reports only direct-child tmp units that pass the frozen ownership,
// exclusion, and layout contract. Production has no admitted layouts, so the
// result is empty even when tmp children exist.
func (a *CodexTmpAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	verdicts, err := a.evaluate(ctx, opts)
	if err != nil {
		return nil, err
	}
	var results []types.DebrisInfo
	for _, verdict := range verdicts {
		if !verdict.Eligible {
			continue
		}
		results = append(results, verdict.item)
	}
	return results, nil
}

type codexTmpVerdict struct {
	Path     string
	Eligible bool
	Reason   string
	item     types.DebrisInfo
	snapshot *codexTmpSnapshot
	tmpRoot  string
}

func (a *CodexTmpAdapter) evaluate(ctx context.Context, opts types.ScanOptions) ([]codexTmpVerdict, error) {
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}
	roots, err = applyCodexHomeScanRoots(opts, roots)
	if err != nil {
		return nil, err
	}
	codexHomes, err := codexhome.Homes()
	if err != nil {
		return nil, err
	}

	var verdicts []codexTmpVerdict
	for _, codexHome := range codexHomes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tmpRoot := filepath.Join(codexHome, codexTmpDirName)
		if !pathUnderRoots(tmpRoot, roots) {
			continue
		}
		homeVerdicts, err := a.evaluateTmpRoot(ctx, tmpRoot)
		if err != nil {
			return nil, err
		}
		verdicts = append(verdicts, homeVerdicts...)
	}
	return verdicts, nil
}

func (a *CodexTmpAdapter) evaluateTmpRoot(ctx context.Context, tmpRoot string) ([]codexTmpVerdict, error) {
	info, err := os.Lstat(tmpRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return []codexTmpVerdict{{
			Path:   tmpRoot,
			Reason: codexTmpReasonSymlinkRoot,
		}}, nil
	}
	if !info.IsDir() {
		return []codexTmpVerdict{{
			Path:   tmpRoot,
			Reason: codexTmpReasonNotDirectory,
		}}, nil
	}

	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	verdicts := make([]codexTmpVerdict, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		child := filepath.Join(tmpRoot, entry.Name())
		verdicts = append(verdicts, a.evaluateChild(ctx, tmpRoot, child))
	}
	return verdicts, nil
}

func (a *CodexTmpAdapter) evaluateChild(ctx context.Context, tmpRoot, child string) codexTmpVerdict {
	verdict := codexTmpVerdict{Path: child, tmpRoot: tmpRoot}
	info, err := os.Lstat(child)
	if err != nil {
		verdict.Reason = codexTmpReasonUnknownEntry
		return verdict
	}
	if info.Mode()&os.ModeSymlink != 0 {
		verdict.Reason = codexTmpReasonSymlinkChild
		return verdict
	}
	if !info.IsDir() {
		verdict.Reason = codexTmpReasonUnknownEntry
		return verdict
	}

	fence, reason := a.acquireExclusion(child)
	if fence == nil {
		verdict.Reason = reason
		return verdict
	}
	defer fence.Release()

	members, err := inventoryCodexTmpUnit(ctx, child)
	if err != nil {
		verdict.Reason = codexTmpReasonInventory
		if errors.Is(err, errCodexTmpUnknownSymlink) {
			verdict.Reason = codexTmpReasonUnknownSymlink
		}
		return verdict
	}
	if reason := a.ownershipReason(members); reason != "" {
		verdict.Reason = reason
		return verdict
	}
	if !fence.Held() {
		verdict.Reason = codexTmpReasonFenceLost
		return verdict
	}

	snapshot := &codexTmpSnapshot{
		CanonicalPath: child,
		FenceToken:    fence.Token(),
		Members:       members,
	}
	activity := estimateDirActivity(ctx, child)
	modTime := info.ModTime()
	if activity.NewestModTime.After(modTime) {
		modTime = activity.NewestModTime
	}
	verdict.Eligible = true
	verdict.snapshot = snapshot
	verdict.item = types.DebrisInfo{
		Tool:        types.ToolCodex,
		Category:    types.CategoryOtherCache,
		ID:          "tmp-" + filepath.Base(child),
		Source:      ".codex",
		Path:        child,
		Size:        activity.Size,
		ModTime:     modTime,
		PathModTime: info.ModTime(),
	}
	return verdict
}

func (a *CodexTmpAdapter) acquireExclusion(unit string) (*codexTmpFence, string) {
	if a.exclusion == nil {
		return nil, codexTmpReasonNoExclusion
	}
	if !a.exclusion.complete() {
		return nil, codexTmpReasonIncompleteWriters
	}
	fence, err := a.exclusion.Acquire(unit)
	if err != nil {
		return nil, codexTmpReasonNoExclusion
	}
	return fence, ""
}

func (a *CodexTmpAdapter) ownershipReason(members []codexTmpMember) string {
	if strings.TrimSpace(a.version) == "" {
		return codexTmpReasonUnknownVersion
	}
	for _, layout := range a.layouts {
		if layout.Version != a.version {
			continue
		}
		if reason := layout.account(members); reason != "" {
			return reason
		}
		return ""
	}
	return codexTmpReasonUnknownLayout
}

func (a *CodexTmpAdapter) tmpRootForUnit(unit string) (string, error) {
	homes, err := codexhome.Homes()
	if err != nil {
		return "", err
	}
	cleanUnit := filepath.Clean(unit)
	for _, home := range homes {
		tmpRoot := filepath.Clean(filepath.Join(home, codexTmpDirName))
		if cleanUnit == tmpRoot {
			return tmpRoot, fmt.Errorf("%s", codexTmpReasonTmpRoot)
		}
		if filepath.Dir(cleanUnit) == tmpRoot {
			return tmpRoot, nil
		}
	}
	return "", fmt.Errorf("%s", codexTmpReasonNotDirectChild)
}
