package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type cleanExperience string

const (
	cleanExperienceClassic cleanExperience = "classic"
	cleanExperienceGuided  cleanExperience = "guided-codex"

	guidedCodexCleanupPressureMinSize       int64 = 256 * 1024 * 1024
	guidedCodexCleanupPressureUnitThreshold       = 3

	guidedCleanReasonAuto     = "active worktrees are the largest cleanup decision"
	guidedCleanReasonExplicit = "requested by --guide"
)

type cleanExperienceInput struct {
	Guide                         bool
	NoGuide                       bool
	CategoryChanged               bool
	ToolChanged                   bool
	RiskyChanged                  bool
	ForceChanged                  bool
	IncludeActiveWorktreesChanged bool
	InteractiveChanged            bool
	UsefulGuidedCodexReview       bool
}

func cleanExperienceInputFromCommand(cmd *cobra.Command, usefulGuidedCodexReview bool) cleanExperienceInput {
	return cleanExperienceInput{
		Guide:                         cleanGuide,
		NoGuide:                       cleanNoGuide,
		CategoryChanged:               cmd.Flags().Changed("category"),
		ToolChanged:                   cmd.Flags().Changed("tool"),
		RiskyChanged:                  cmd.Flags().Changed("risky"),
		ForceChanged:                  cmd.Flags().Changed("force"),
		IncludeActiveWorktreesChanged: cmd.Flags().Changed("include-active-worktrees"),
		InteractiveChanged:            cmd.Flags().Changed("interactive"),
		UsefulGuidedCodexReview:       usefulGuidedCodexReview,
	}
}

func chooseCleanExperience(input cleanExperienceInput) (cleanExperience, string, error) {
	if input.Guide && input.NoGuide {
		return cleanExperienceClassic, "", fmt.Errorf("cannot use --guide with --no-guide")
	}
	if input.Guide {
		return cleanExperienceGuided, guidedCleanReasonExplicit, nil
	}
	if input.NoGuide || input.hasClassicSelector() {
		return cleanExperienceClassic, "", nil
	}
	if input.UsefulGuidedCodexReview {
		return cleanExperienceGuided, guidedCleanReasonAuto, nil
	}
	return cleanExperienceClassic, "", nil
}

func (input cleanExperienceInput) hasClassicSelector() bool {
	return input.CategoryChanged ||
		input.ToolChanged ||
		input.RiskyChanged ||
		input.ForceChanged ||
		input.IncludeActiveWorktreesChanged ||
		input.InteractiveChanged
}

func shouldPrepareGuidedClean(cmd *cobra.Command) bool {
	if cleanGuide {
		return true
	}
	if cleanNoGuide {
		return false
	}
	return !cleanExperienceInputFromCommand(cmd, false).hasClassicSelector()
}

func hasGuidedCodexCleanupPressure(ctx context.Context, items []types.DebrisInfo) bool {
	unitCount, totalSize := guidedCodexCleanupPressure(ctx, items)
	return isGuidedCodexCleanupPressureValuable(unitCount, totalSize)
}

func isGuidedCodexCleanupPressureValuable(unitCount int, totalSize int64) bool {
	return unitCount > 0 && (totalSize >= guidedCodexCleanupPressureMinSize || unitCount >= guidedCodexCleanupPressureUnitThreshold)
}

func guidedCodexCleanupPressure(ctx context.Context, items []types.DebrisInfo) (int, int64) {
	// Pressure is measured over every tool's active worktrees, matching what
	// guided review will actually show once it opens.
	candidates := activeWorktrees(items)

	units, err := worktree.BuildWorktreeCleanupUnits(ctx, candidates)
	if err != nil || len(units) == 0 {
		return 0, 0
	}

	var totalSize int64
	for _, unit := range units {
		totalSize += unit.Size
	}
	return len(units), totalSize
}
