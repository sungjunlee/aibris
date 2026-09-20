package worktree

import (
	"context"
	"fmt"
	"io"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// PreparedActiveWorktreeTarget holds the evidence needed to execute one active
// worktree cleanup target with overlap safety and snapshot validation.
type PreparedActiveWorktreeTarget struct {
	Item     types.DebrisInfo
	Unit     WorktreeCleanupUnit
	Snapshot *cleaner.CleanupTargetSnapshot
	// BeforeMutation runs before any physical mutation. A non-nil error refuses
	// the entire cleanup target.
	BeforeMutation func(context.Context) error
	// AfterMember runs after each member is removed. remaining is the number of
	// members not yet processed. A non-nil error stops execution.
	AfterMember func(ctx context.Context, remaining int) error
}

// ActiveWorktreeExecutionResult reports the outcome of executing one prepared
// active worktree target. Cmd maps this to receipts and stdout.
type ActiveWorktreeExecutionResult struct {
	Members           []MemberExecution
	PhysicalRemoved   bool
	MutationAttempted bool
	StartedMembers    bool
	BlockingPath      string
	BlockingReason    string
}

// ExecutePreparedActiveWorktreeTarget executes one prepared active worktree
// cleanup target with validation barriers. It refreshes Git evidence during
// preflight, validates snapshots before mutation, and tracks physical outcomes.
func ExecutePreparedActiveWorktreeTarget(
	ctx context.Context,
	prepared PreparedActiveWorktreeTarget,
	opts ExecutionOptions,
) (ActiveWorktreeExecutionResult, error) {
	result := ActiveWorktreeExecutionResult{
		Members: memberExecutions(prepared.Unit.Members),
	}

	unitResult, err := ExecuteActiveWorktreeUnit(ctx, prepared.Item, prepared.Unit, ExecutionOptions{
		RemoveWorktree: opts.RemoveWorktree,
		RemoveAll:      opts.RemoveAll,
		Getwd:          opts.Getwd,
		UserHomeDir:    opts.UserHomeDir,
		BeforeMutation: func(ctx context.Context) error {
			if prepared.BeforeMutation != nil {
				if validationErr := prepared.BeforeMutation(ctx); validationErr != nil {
					return fmt.Errorf("pre-mutation safety barrier: %w", validationErr)
				}
			}
			if prepared.Snapshot == nil {
				return fmt.Errorf("pre-mutation safety barrier: cleanup target snapshot unavailable")
			}
			// snapshot is an active worktree unit here, so it is never
			// activity-derived and validate cannot walk the tree per member.
			if snapshotErr := prepared.Snapshot.Validate(ctx); snapshotErr != nil {
				result.BlockingPath = prepared.Item.Path
				result.BlockingReason = snapshotErr.Error()
				return fmt.Errorf("pre-mutation safety barrier: %v", snapshotErr)
			}
			return nil
		},
		AfterMember: func(ctx context.Context, remaining int) error {
			ownerRemoved, snapshotErr := prepared.Snapshot.RefreshAfterMutation()
			if snapshotErr != nil {
				result.BlockingPath = prepared.Item.Path
				result.BlockingReason = snapshotErr.Error()
				return snapshotErr
			}
			if ownerRemoved && remaining > 0 {
				err := fmt.Errorf("cleanup target disappeared before removing remaining worktree members: %q", prepared.Item.Path)
				result.BlockingPath = prepared.Item.Path
				result.BlockingReason = err.Error()
				return err
			}
			if prepared.AfterMember != nil {
				return prepared.AfterMember(ctx, remaining)
			}
			return nil
		},
		RemovingMember: opts.RemovingMember,
		RemovedMember:  opts.RemovedMember,
	})

	result.Members = unitResult.Members
	result.PhysicalRemoved = unitResult.PhysicalRemoved
	result.MutationAttempted = unitResult.MutationAttempted
	result.StartedMembers = unitResult.StartedMembers

	if err != nil {
		if unitResult.StartedMembers {
			setResultPhysicalState(&result, prepared.Unit)
		} else if result.BlockingPath == "" {
			result.BlockingPath = prepared.Item.Path
			result.BlockingReason = err.Error()
		}
		return result, err
	}

	return result, nil
}

func setResultPhysicalState(result *ActiveWorktreeExecutionResult, unit WorktreeCleanupUnit) {
	result.PhysicalRemoved = PathDoesNotExist(unit.TargetPath)
}

// WritePreparedActiveWorktreeSuccess writes the standard success message for
// one completed active worktree cleanup. The name parameter should be derived
// from target.ID, target.Project, or filepath.Base(target.Path).
func WritePreparedActiveWorktreeSuccess(
	output io.Writer,
	name string,
	tool types.Tool,
	freedBytes int64,
) {
	fmt.Fprintf(output, "removed: %s (%s) — %s\n", name, tool, cleaner.FormatSize(freedBytes))
}
