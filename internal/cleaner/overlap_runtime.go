package cleaner

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sungjunlee/aibris/internal/adapter"
)

// RefreshMemo caches one full agent-state re-scan for the lifetime of a single
// execution batch, keyed by a cheap entry-set fingerprint so that additions,
// removals, or renames of agent-state entries invalidate the cache and force a
// fresh scan. It is safe for concurrent use; executePreparedCleanTargets runs
// targets sequentially today, but the barrier contract does not require it.
type RefreshMemo struct {
	mu       sync.Mutex
	loaded   bool
	key      string
	evidence OverlapSafetyEvidence
}

// Get returns the cached evidence when the fingerprint key is unchanged,
// otherwise runs refresh and caches a successful result. Errors are never
// cached: a failed scan leaves the previous state untouched so the next target
// retries instead of inheriting one target's transient failure. A fingerprint
// error fails closed by forcing a rescan.
func (m *RefreshMemo) Get(
	ctx context.Context,
	fingerprint func(context.Context) (string, error),
	refresh func(context.Context) (OverlapSafetyEvidence, error),
) (OverlapSafetyEvidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := ""
	if fingerprint != nil {
		if fp, err := fingerprint(ctx); err == nil {
			key = fp
		}
		// On fingerprint error key stays "" so the cache is never reused
		// (fail closed: an unverifiable entry set must trigger a fresh scan).
	}
	if m.loaded && fingerprint != nil && key != "" && key == m.key {
		return m.evidence, nil
	}
	evidence, err := refresh(ctx)
	if err != nil {
		return evidence, err
	}
	m.evidence = evidence
	m.key = key
	m.loaded = true
	return evidence, nil
}

func (m *RefreshMemo) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loaded = false
	m.key = ""
	m.evidence = OverlapSafetyEvidence{}
}

// OverlapRuntime bundles the overlap-safety evidence sources used by the clean
// command: the initial evidence snapshot backing BuildOverlapSafetyPlan, the
// batch re-scan used for per-target mutation barriers, the agent-state
// revalidator lookup, and the memoization machinery that collapses repeated
// full re-scans within one execution batch.
type OverlapRuntime struct {
	Initial OverlapSafetyEvidence
	Refresh func(context.Context) (OverlapSafetyEvidence, error)
	Lookup  AgentStateRevalidatorLookup
	// Memo, when non-nil, collapses the expensive full agent-state re-scan
	// (Refresh) so per-target mutation barriers within one execution batch
	// share a single scan instead of re-running it per target. The cached scan
	// is only reused while Fingerprint reports an unchanged agent-state entry
	// set; any added, removed, or renamed entry invalidates it and forces a
	// fresh full scan so newly created overlapping state is still discovered
	// before the next mutation. Classification drift of entries already known
	// to overlap a target is independently caught by the per-obligation
	// RevalidateAgentState inside ValidateBeforeMutationWithReport, which runs
	// live for every target. A nil Memo disables memoization.
	Memo *RefreshMemo
	// Fingerprint cheaply enumerates the current agent-state entry set (entry
	// directory names under the agent-state store roots, no jsonl parsing or
	// size walking). It enumerates the roots from adapter.AgentStateStoreRoots(),
	// the single source of truth shared with the agent-state providers, so a
	// newly added agent-state root automatically flows to the fingerprint.
	Fingerprint func(context.Context) (string, error)
}

// NewOverlapRuntime wires an OverlapRuntime with a fresh batch-scoped refresh
// memo and the default agent-state entry fingerprint.
func NewOverlapRuntime(
	initial OverlapSafetyEvidence,
	refresh func(context.Context) (OverlapSafetyEvidence, error),
	lookup AgentStateRevalidatorLookup,
) OverlapRuntime {
	return OverlapRuntime{
		Initial:     initial,
		Refresh:     refresh,
		Lookup:      lookup,
		Memo:        &RefreshMemo{},
		Fingerprint: AgentStateEntryFingerprint,
	}
}

// RefreshedEvidence returns the refreshed overlap evidence, memoizing the full
// re-scan across a batch when a memo is configured and the entry-set
// fingerprint is unchanged.
func (r OverlapRuntime) RefreshedEvidence(ctx context.Context) (OverlapSafetyEvidence, error) {
	if r.Memo != nil {
		return r.Memo.Get(ctx, r.Fingerprint, r.Refresh)
	}
	return r.Refresh(ctx)
}

// ResetRefreshMemo clears any cached re-scan so the next batch starts fresh.
func (r OverlapRuntime) ResetRefreshMemo() {
	if r.Memo != nil {
		r.Memo.reset()
	}
}

// AgentStateEntryFingerprint enumerates the current agent-state entry set by
// listing immediate child directory names under each agent-state store root.
// This mirrors how the Claude/Cursor providers enumerate entries (each child
// directory is one entry) without parsing jsonl or walking sizes, so it is
// cheap enough to run before every mutation. Adding, removing, or renaming an
// entry changes the returned key. Names are joined with \x00, which cannot
// appear in a filename, so the key is injective in the entry set (a directory
// named "a,b" can never alias two directories "a" and "b").
func AgentStateEntryFingerprint(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	roots, err := adapter.AgentStateStoreRoots()
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(roots))
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				parts = append(parts, root+"\x00<absent>")
				continue
			}
			return "", err
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		sort.Strings(names)
		parts = append(parts, root+"\x00"+strings.Join(names, "\x00"))
	}
	return strings.Join(parts, "|"), nil
}
