package executor

import (
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// ExecutionState represents the outcome state of a cleanup execution.
type ExecutionState string

const (
	ExecutionRemoved   ExecutionState = "removed"
	ExecutionPartial   ExecutionState = "partial"
	ExecutionFailed    ExecutionState = "failed"
	ExecutionCancelled ExecutionState = "cancelled"
)

// MemberExecutionReceipt reports the outcome of removing one worktree member.
type MemberExecutionReceipt struct {
	WorktreePath string
	Removed      bool
	Error        string
}

// UnitExecutionReceipt reports the outcome of executing one cleanup target.
type UnitExecutionReceipt struct {
	Target                     types.DebrisInfo
	ReceiptTargetKey           string
	Component                  *cleaner.CleanupOverlapComponent
	State                      ExecutionState
	PhysicalRemoved            bool
	FreedBytes                 int64
	ResidualBytes              int64
	Members                    []MemberExecutionReceipt
	Obligations                []cleaner.AgentStateRevalidationOutcome
	BlockingPath               string
	BlockingReason             string
	MutationAttempted          bool
	CommandFallbackPathRemoval bool
	Error                      string
	// FailureCause keeps the failure's error chain alongside its rendered
	// message so the JSON projection can classify it with errors.Is.
	FailureCause error
}

// ExecutionReceipt reports the aggregate outcome of a cleanup execution.
type ExecutionReceipt struct {
	Units      []UnitExecutionReceipt
	FreedBytes int64
}

// Counts returns the number of removed, partial, and failed units.
func (r ExecutionReceipt) Counts() (removed, partial, failed int) {
	for _, unit := range r.Units {
		switch unit.State {
		case ExecutionRemoved:
			removed++
		case ExecutionPartial:
			partial++
		case ExecutionFailed, ExecutionCancelled:
			failed++
		}
	}
	return removed, partial, failed
}

// ApplyActiveUnitExecutionReceipt updates the receipt with active worktree execution results.
func ApplyActiveUnitExecutionReceipt(receipt *UnitExecutionReceipt, result worktree.UnitExecution) {
	receipt.MutationAttempted = receipt.MutationAttempted || result.MutationAttempted
	receipt.PhysicalRemoved = result.PhysicalRemoved
	if len(result.Members) == 0 {
		return
	}
	receipt.Members = make([]MemberExecutionReceipt, len(result.Members))
	for i, member := range result.Members {
		receipt.Members[i] = MemberExecutionReceipt{
			WorktreePath: member.WorktreePath,
			Removed:      member.Removed,
			Error:        member.Error,
		}
	}
}

// ApplyPreparedActiveWorktreeExecutionResult updates the receipt with prepared active worktree execution results.
func ApplyPreparedActiveWorktreeExecutionResult(receipt *UnitExecutionReceipt, result worktree.ActiveWorktreeExecutionResult) {
	receipt.MutationAttempted = receipt.MutationAttempted || result.MutationAttempted
	receipt.PhysicalRemoved = result.PhysicalRemoved
	if result.BlockingPath != "" {
		receipt.BlockingPath = result.BlockingPath
		receipt.BlockingReason = result.BlockingReason
	}
	if len(result.Members) == 0 {
		return
	}
	receipt.Members = make([]MemberExecutionReceipt, len(result.Members))
	for i, member := range result.Members {
		receipt.Members[i] = MemberExecutionReceipt{
			WorktreePath: member.WorktreePath,
			Removed:      member.Removed,
			Error:        member.Error,
		}
	}
}

// SetActiveReceiptPhysicalState sets the physical state based on actual filesystem state.
func SetActiveReceiptPhysicalState(receipt *UnitExecutionReceipt, selected worktree.WorktreeCleanupUnit) {
	receipt.PhysicalRemoved = worktree.PathDoesNotExist(selected.TargetPath)
	if receipt.PhysicalRemoved && receipt.MutationAttempted {
		receipt.FreedBytes = selected.Size
	}
	removedMembers := 0
	for _, member := range receipt.Members {
		if member.Removed {
			removedMembers++
		}
	}
	if removedMembers > 0 || (receipt.PhysicalRemoved && receipt.MutationAttempted) {
		receipt.State = ExecutionPartial
	} else {
		receipt.State = ExecutionFailed
	}
}

// FailedCleanUnitReceipt creates a failed receipt for a cleanup unit.
func FailedCleanUnitReceipt(target types.DebrisInfo, members []worktree.GitWorktreeMember, err error, receiptKeyFn func(types.DebrisInfo) string) UnitExecutionReceipt {
	receipt := UnitExecutionReceipt{
		Target:           target,
		ReceiptTargetKey: receiptKeyFn(target),
		State:            ExecutionFailed,
		Error:            err.Error(),
	}
	for _, member := range members {
		receipt.Members = append(receipt.Members, MemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}
	return receipt
}

// FailedPreparedCleanUnitReceipt creates a failed receipt for a prepared cleanup target.
func FailedPreparedCleanUnitReceipt(
	item types.DebrisInfo,
	component *cleaner.CleanupOverlapComponent,
	err error,
	receiptKeyFn func(types.DebrisInfo) string,
) UnitExecutionReceipt {
	receipt := UnitExecutionReceipt{
		Target:           item,
		ReceiptTargetKey: receiptKeyFn(item),
		Component:        component,
		State:            ExecutionFailed,
		BlockingPath:     item.Path,
		BlockingReason:   err.Error(),
		Error:            err.Error(),
		FailureCause:     err,
	}
	if component != nil {
		for _, obligation := range component.Obligations {
			receipt.Obligations = append(receipt.Obligations, cleaner.AgentStateRevalidationOutcome{
				Tool:       obligation.Tool,
				EntryPath:  obligation.EntryPath,
				ProviderID: obligation.ProviderID,
				State:      cleaner.AgentStateRevalidationNotAttempted,
			})
		}
	}
	return receipt
}

// CancelledPreparedCleanUnitReceipt creates a cancelled receipt for a prepared cleanup target.
func CancelledPreparedCleanUnitReceipt(
	item types.DebrisInfo,
	component *cleaner.CleanupOverlapComponent,
	err error,
	receiptKeyFn func(types.DebrisInfo) string,
) UnitExecutionReceipt {
	receipt := FailedPreparedCleanUnitReceipt(item, component, err, receiptKeyFn)
	receipt.State = ExecutionCancelled
	return receipt
}

// NewCleanUnitExecutionReceipt creates a new execution receipt for a cleanup unit.
func NewCleanUnitExecutionReceipt(
	target types.DebrisInfo,
	component *cleaner.CleanupOverlapComponent,
	safety *cleaner.CleanupMutationSafety,
	receiptKeyFn func(types.DebrisInfo) string,
) UnitExecutionReceipt {
	receipt := UnitExecutionReceipt{
		Target:           target,
		ReceiptTargetKey: receiptKeyFn(target),
		Component:        component,
		State:            ExecutionFailed,
	}
	if component != nil {
		for _, obligation := range component.Obligations {
			receipt.Obligations = append(receipt.Obligations, cleaner.AgentStateRevalidationOutcome{
				Tool:       obligation.Tool,
				EntryPath:  obligation.EntryPath,
				ProviderID: obligation.ProviderID,
				State:      cleaner.AgentStateRevalidationNotAttempted,
			})
		}
		return receipt
	}
	if safety != nil {
		validation := cleaner.InitialOverlapSafetyValidation(safety.Component)
		receipt.Obligations = validation.Obligations
	}
	return receipt
}

// ApplyOverlapValidationReceipt updates the receipt with overlap safety validation results.
func ApplyOverlapValidationReceipt(
	receipt *UnitExecutionReceipt,
	validation cleaner.OverlapSafetyValidation,
) {
	receipt.Obligations = append(
		receipt.Obligations[:0],
		validation.Obligations...,
	)
	receipt.BlockingPath = validation.BlockingPath
	receipt.BlockingReason = validation.BlockingReason
}

// CleanUnitHasMutation returns true if the unit has any physical mutations.
func CleanUnitHasMutation(receipt UnitExecutionReceipt) bool {
	if receipt.PhysicalRemoved {
		return true
	}
	for _, member := range receipt.Members {
		if member.Removed {
			return true
		}
	}
	return false
}
