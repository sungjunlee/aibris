package cleaner

import (
	"context"
	"io"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

// Execute removes the given worktrees from disk.
func Execute(worktrees []types.DebrisInfo) (int64, error) {
	return ExecuteWithContext(context.Background(), worktrees)
}

// ExecuteWithContext removes or command-cleans the given debris items from disk.
func ExecuteWithContext(ctx context.Context, worktrees []types.DebrisInfo) (int64, error) {
	return executeWithContext(ctx, worktrees, adapter.AgentStateRevalidatorFor, nil)
}

// MutationBarrier runs immediately before a cleanup command or filesystem
// removal. It must be read-only and return an error to refuse the mutation.
type MutationBarrier func(context.Context, types.DebrisInfo) error

// CleanupMutationOutcome reports an execution attempt made immediately after
// the mutation barrier. Observers are informational and cannot affect cleanup
// safety or execution.
type CleanupMutationOutcome struct {
	Item                       types.DebrisInfo
	MutationAttempted          bool
	CommandFallbackPathRemoval bool
	FreedBytes                 int64
	ResidualBytes              int64
}

type CleanupMutationObserver func(CleanupMutationOutcome)

// ExecuteWithContextAndBarrier executes cleanup only after the supplied
// component-level safety barrier succeeds at the first mutation boundary.
func ExecuteWithContextAndBarrier(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	barrier MutationBarrier,
) (int64, error) {
	return executeWithContext(ctx, worktrees, adapter.AgentStateRevalidatorFor, barrier)
}

// ExecuteWithContextAndBarrierWithOutput preserves the cleanup executor while
// allowing callers to choose where its progress and diagnostics go.
func ExecuteWithContextAndBarrierWithOutput(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	barrier MutationBarrier,
	output io.Writer,
	errorOutput io.Writer,
) (int64, error) {
	return ExecuteWithContextAndBarrierWithOutputAndObserver(ctx, worktrees, barrier, output, errorOutput, nil)
}

// ExecuteWithContextAndBarrierWithOutputAndObserver reports each command or
// path-removal attempt immediately after its mutation barrier succeeds.
func ExecuteWithContextAndBarrierWithOutputAndObserver(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	barrier MutationBarrier,
	output io.Writer,
	errorOutput io.Writer,
	observer CleanupMutationObserver,
) (int64, error) {
	return executeWithContextOutput(ctx, worktrees, adapter.AgentStateRevalidatorFor, barrier, output, errorOutput, observer)
}
