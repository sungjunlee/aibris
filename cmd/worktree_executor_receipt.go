package cmd

import (
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type cleanExecutionState string

const (
	cleanExecutionRemoved   cleanExecutionState = "removed"
	cleanExecutionPartial   cleanExecutionState = "partial"
	cleanExecutionFailed    cleanExecutionState = "failed"
	cleanExecutionCancelled cleanExecutionState = "cancelled"
)

type cleanMemberExecutionReceipt struct {
	WorktreePath string
	Removed      bool
	Error        string
}

type cleanUnitExecutionReceipt struct {
	Target                     types.DebrisInfo
	ReceiptTargetKey           string
	Component                  *cleanupOverlapComponent
	State                      cleanExecutionState
	PhysicalRemoved            bool
	FreedBytes                 int64
	ResidualBytes              int64
	Members                    []cleanMemberExecutionReceipt
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

type cleanExecutionReceipt struct {
	Units      []cleanUnitExecutionReceipt
	FreedBytes int64
}

func (r cleanExecutionReceipt) counts() (removed, partial, failed int) {
	for _, unit := range r.Units {
		switch unit.State {
		case cleanExecutionRemoved:
			removed++
		case cleanExecutionPartial:
			partial++
		case cleanExecutionFailed, cleanExecutionCancelled:
			failed++
		}
	}
	return removed, partial, failed
}

func applyActiveUnitExecutionReceipt(receipt *cleanUnitExecutionReceipt, result worktree.UnitExecution) {
	receipt.MutationAttempted = receipt.MutationAttempted || result.MutationAttempted
	receipt.PhysicalRemoved = result.PhysicalRemoved
	if len(result.Members) == 0 {
		return
	}
	receipt.Members = make([]cleanMemberExecutionReceipt, len(result.Members))
	for i, member := range result.Members {
		receipt.Members[i] = cleanMemberExecutionReceipt{
			WorktreePath: member.WorktreePath,
			Removed:      member.Removed,
			Error:        member.Error,
		}
	}
}

func setActiveReceiptPhysicalState(receipt *cleanUnitExecutionReceipt, selected worktree.WorktreeCleanupUnit) {
	receipt.PhysicalRemoved = pathDoesNotExist(selected.TargetPath)
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
		receipt.State = cleanExecutionPartial
	} else {
		receipt.State = cleanExecutionFailed
	}
}

func failedCleanUnitReceipt(target types.DebrisInfo, members []worktree.GitWorktreeMember, err error) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{
		Target:           target,
		ReceiptTargetKey: cleanJSONReceiptItemKey(target),
		State:            cleanExecutionFailed,
		Error:            err.Error(),
	}
	for _, member := range members {
		receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}
	return receipt
}

func failedPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{
		Target:           target.Item,
		ReceiptTargetKey: cleanJSONReceiptItemKey(target.Item),
		Component:        target.Component,
		State:            cleanExecutionFailed,
		BlockingPath:     target.Item.Path,
		BlockingReason:   err.Error(),
		Error:            err.Error(),
		FailureCause:     err,
	}
	if target.Component != nil {
		for _, obligation := range target.Component.Obligations {
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

func cancelledPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	receipt := failedPreparedCleanUnitReceipt(target, err)
	receipt.State = cleanExecutionCancelled
	return receipt
}

func newCleanUnitExecutionReceipt(
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	safety *cleanupMutationSafety,
) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{
		Target:           target,
		ReceiptTargetKey: cleanJSONReceiptItemKey(target),
		Component:        component,
		State:            cleanExecutionFailed,
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
		validation := initialOverlapSafetyValidation(safety.component)
		receipt.Obligations = validation.Obligations
	}
	return receipt
}

func applyOverlapValidationReceipt(
	receipt *cleanUnitExecutionReceipt,
	validation cleaner.OverlapSafetyValidation,
) {
	receipt.Obligations = append(
		receipt.Obligations[:0],
		validation.Obligations...,
	)
	receipt.BlockingPath = validation.BlockingPath
	receipt.BlockingReason = validation.BlockingReason
}

func cleanUnitHasMutation(receipt cleanUnitExecutionReceipt) bool {
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
