package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	cleanAge                    string
	cleanCategory               string
	cleanTools                  string
	cleanDryRun                 bool
	cleanJSON                   bool
	cleanIncludePaths           bool
	cleanInteractive            bool
	cleanRisky                  bool
	cleanForce                  bool
	cleanGuide                  bool
	cleanNoGuide                bool
	cleanStrip                  bool
	cleanRoots                  []string
	cleanExcludes               []string
	cleanIncludeActiveWorktrees bool
	cleanAgentStateGrace        string
	cleanReceiptFile            string
	cleanAPFSSnapshots          bool
	cleanPressure               bool
)

// errClassicRouteReceiptFile is shared by the pre-scan flag check and the
// post-scan route check so both refusals read identically.
const errClassicRouteReceiptFile = "error: --receipt-file is not available on the classic route; use --json for a classic receipt"

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up old AI tool debris",
	Long: `Clean up old AI tool debris.

With no classic cleanup filters, clean uses guided worktree review by default when useful.
Guided worktree choices and classic candidates merge into one unified review and execution plan.
Use --no-guide, or pass an explicit classic selector such as --category, --tool,
--risky, --force, --include-active-worktrees, or --interactive to keep the
classic cleanup audit and executor route. JSON execution is always classic;
--guide is available with JSON only for --dry-run plans.

--receipt-file writes the machine-readable execution receipt of a guided or
--json execution run to a file while stdout keeps its usual shape. It requires
an execution run and is not available on the classic route, which already has
--json.

Across both routes, selected targets enter the cleanup plan, reviewable targets
require explicit selection, and protected targets never enter the plan. Guided
review displays protected targets as locked rows.

--strip is a separate disposition from deletion: it removes only the
regenerable subtrees (dependency directories and platform build output)
inventoried inside worktree units that deletion protects, recovering space
without deleting the unit, its branch, or any uncommitted work.`,
	Run: func(cmd *cobra.Command, args []string) {
		if cleanIncludePaths && !cleanJSON && cleanReceiptFile == "" {
			fmt.Fprintln(os.Stderr, "error: --include-paths requires --json")
			os.Exit(1)
		}
		if cleanReceiptFile != "" && cleanDryRun {
			fmt.Fprintln(os.Stderr, "error: --receipt-file requires an execution run (remove --dry-run)")
			os.Exit(1)
		}
		if cleanGuide && cleanNoGuide {
			fmt.Fprintln(os.Stderr, "error: cannot use --guide with --no-guide")
			os.Exit(1)
		}
		if cleanStrip && cleanAPFSSnapshots {
			fmt.Fprintln(os.Stderr, "error: --strip cannot be combined with --apfs-snapshots")
			os.Exit(1)
		}
		if cleanAPFSSnapshots {
			if err := apfsSnapshotFlagConflict(cmd); err != "" {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			runAPFSSnapshotClean()
			return
		}
		if cleanStrip {
			if cleanJSON || cleanInteractive || cleanGuide || cleanReceiptFile != "" {
				fmt.Fprintln(os.Stderr, "error: --strip cannot be combined with --json, --interactive, --guide, or --receipt-file")
				os.Exit(1)
			}
			runStripClean()
			return
		}
		if cleanReceiptFile != "" && cleanNoGuide && !cleanJSON {
			fmt.Fprintln(os.Stderr, errClassicRouteReceiptFile)
			os.Exit(1)
		}
		if cleanJSON {
			if !cleanDryRun && cleanGuide {
				fmt.Fprintln(os.Stderr, "error: non-dry-run --json cannot use --guide")
				os.Exit(1)
			}
			if !cleanDryRun && !cleanForce && !cleanInteractive {
				fmt.Fprintln(os.Stderr, "error: non-dry-run --json requires --force or --interactive")
				os.Exit(1)
			}
			runCleanJSON(cmd)
			return
		}
		age, err := parseAge(cleanAge)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid age '%s': expected duration like 7d, 2w, 1mo, 1y, or 24h\n", cleanAge)
			os.Exit(1)
		}

		if age <= 0 {
			fmt.Fprintf(os.Stderr, "error: --age must be positive (got %s)\n", cleanAge)
			os.Exit(1)
		}
		agentStateGrace, err := parseAge(cleanAgentStateGrace)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid agent-state grace '%s': expected duration like 24h, 2d, 1w, or 0\n", cleanAgentStateGrace)
			os.Exit(1)
		}
		if agentStateGrace < 0 {
			fmt.Fprintf(os.Stderr, "error: --agent-state-grace must be non-negative (got %s)\n", cleanAgentStateGrace)
			os.Exit(1)
		}
		guidedAge := guidedCleanAge(cmd, age)
		if cleanGuide {
			age = applyGuidedCleanDefaults(cmd, age)
			guidedAge = age
		}
		categories, err := parseCleanCategories(cleanCategory)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		tools, err := parseCleanTools(cleanTools)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		roots, err := scanner.NormalizeRoots(cleanRoots)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printCleanHeader(roots)

		result, source, err := scanForClean(ctx, roots, cleanExcludes, len(cleanRoots) > 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printExclusionDiagnostics(result)
		refreshCleanupInventoryMetadataWithContext(ctx, result.Worktrees)
		overlapSafety, err := newDefaultCleanupOverlapSafetyRuntime(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: preparing overlap safety: %v\n", err)
			os.Exit(1)
		}

		var guidedState guidedCleanState
		usefulGuidedCodexReview := false
		if shouldPrepareGuidedClean(cmd) {
			usefulGuidedCodexReview = hasGuidedCodexCleanupPressure(ctx, result.Worktrees)
		}
		if cleanGuide || usefulGuidedCodexReview {
			guidedState, err = buildGuidedCleanState(ctx, result, source, guidedAge, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: preparing guided cleanup: %v\n", err)
				os.Exit(1)
			}
		}
		experience, reason, err := chooseCleanExperience(cleanExperienceInputFromCommand(cmd, usefulGuidedCodexReview))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if experience == cleanExperienceClassic && age < time.Hour {
			fmt.Fprintf(
				os.Stderr,
				"Warning: --age %s is a very low classic minimum-age threshold; it broadens age-eligible items within the selected category/tool scope, but risky-category, active-worktree, agent-state, overlap, and Git safety protections still apply.\n",
				cleanAge,
			)
		}

		opts := types.PruneOptions{
			Age:                    age,
			Categories:             categories,
			Tools:                  tools,
			DryRun:                 cleanDryRun,
			Interactive:            cleanInteractive,
			Risky:                  cleanRisky,
			Force:                  cleanForce,
			IncludeActiveWorktrees: cleanIncludeActiveWorktrees,
			AgentStateMinIdleAge:   agentStateGrace,
		}
		opts.RelaxCacheAge, opts.PressureDevice = shouldRelaxCacheAge(cleanPressure)

		// The route is only settled after the scan. A receipt file requested on
		// a run that resolved to classic fails here, before any mutation.
		if cleanReceiptFile != "" && experience != cleanExperienceGuided {
			fmt.Fprintln(os.Stderr, errClassicRouteReceiptFile)
			os.Exit(1)
		}

		var guidedStatePtr *guidedCleanState
		if experience == cleanExperienceGuided {
			guidedState.Reason = reason
			final, aborted, err := promptGuidedCleanStateForFiles(os.Stdin, os.Stdout, guidedState)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if aborted {
				return
			}
			guidedState = final
			guidedStatePtr = &guidedState
			opts.IncludeActiveWorktrees = false
		}

		targets := cleaner.Filter(result.Worktrees, opts)
		targets, physicalOwnerEligibility := cleaner.ApplyPhysicalOwnerSafety(
			result.Worktrees,
			targets,
			opts.IncludeActiveWorktrees,
		)
		physicalOwnerProtections := cleanAuditReasonsFromEligibility(physicalOwnerEligibility)
		targets = cleaner.FilterExistingTargets(targets)
		targets, scanEvidenceProtections := filterTargetsWithoutScanEvidence(targets)
		targets = cleaner.NormalizeTargets(targets)
		targets, gitSafetyProtections := filterGitUnsafeActiveWorktreeTargets(ctx, targets)
		classicProtections := mergeCleanAuditProtections(
			physicalOwnerProtections,
			scanEvidenceProtections,
			gitSafetyProtections,
		)
		logicalInputs := cleanupOverlapLogicalInputsForAudit(
			result.Worktrees,
			opts,
			classicProtections,
		)
		overlapSelection, err := applyCleanupOverlapSafetyWithRows(
			ctx,
			overlapSafety,
			targets,
			logicalInputs,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: preparing overlap safety: %v\n", err)
			os.Exit(1)
		}
		printOverlapSafetyRefusals(overlapSelection)
		targets = overlapSelection.Targets

		if experience == cleanExperienceGuided {
			runUnifiedGuidedClean(
				ctx,
				result,
				source,
				opts,
				guidedStatePtr,
				targets,
				classicProtections,
				overlapSafety,
				os.Stdin,
				os.Stdout,
			)
			return
		}
		auditTargets := targets
		auditProtections := mergeCleanAuditProtections(
			classicProtections,
			overlapSelection.Protections,
		)
		auditComponents := overlapSelection.Components
		audit := buildPhysicalCleanAuditWithLogicalInputs(
			result.Worktrees,
			auditComponents,
			auditTargets,
			opts,
			len(scanner.DefaultScanner.Providers),
			source,
			auditProtections,
			logicalInputs,
		)
		printCleanAudit(audit, opts)
		printCleanCandidateSummary(targets)

		if len(targets) == 0 {
			fmt.Println("No items to clean.")
			return
		}

		if opts.DryRun {
			printCleanPlanWithComponents(targets, overlapSelection.Components, cleanPlanModeDryRun)
			fmt.Println("[DRY-RUN] No files were removed.")
			return
		}
		prepared := prepareCleanExecutionWithOptions(ctx, overlapSelection, overlapSafety, opts)

		if opts.Interactive {
			receipt, err := interactiveClean(ctx, prepared)
			printWorktreeExecutionReceipts(receipt)
			printCleanupReceipt(len(targets), receipt, audit)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
				os.Exit(1)
			}
			hintAPFSSnapshotsAfterReclaim(receipt.FreedBytes)
			return
		}

		if !opts.Force {
			printCleanPlanWithComponents(targets, overlapSelection.Components, cleanPlanModeDelete)
			if !confirmCleanExecution() {
				return
			}
		}

		receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
		printWorktreeExecutionReceipts(receipt)
		printCleanupReceipt(len(targets), receipt, audit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
			os.Exit(1)
		}
		hintAPFSSnapshotsAfterReclaim(receipt.FreedBytes)
	},
}

var validCleanCategories = []types.Category{
	types.CategoryWorktree,
	types.CategoryNodeModules,
	types.CategoryBuildCache,
	types.CategoryOtherCache,
	types.CategoryAgentState,
	types.CategoryAILogs,
}

var validCleanTools = []types.Tool{
	types.ToolCodex,
	types.ToolClaude,
	types.ToolCursor,
	types.ToolWindsurf,
	types.ToolNodeModules,
	types.ToolUnknown,
	types.ToolBuildCache,
	types.ToolPipCache,
	types.ToolAILogs,
}

func parseCleanCategories(raw string) ([]types.Category, error) {
	values, err := parseCleanSelector(raw, "category", categoryStrings(validCleanCategories))
	if err != nil {
		return nil, err
	}
	categories := make([]types.Category, len(values))
	for i, value := range values {
		categories[i] = types.Category(value)
	}
	return categories, nil
}

func parseCleanTools(raw string) ([]types.Tool, error) {
	values, err := parseCleanSelector(raw, "tool", toolStrings(validCleanTools))
	if err != nil {
		return nil, err
	}
	tools := make([]types.Tool, len(values))
	for i, value := range values {
		tools[i] = types.Tool(value)
	}
	return tools, nil
}

func parseCleanSelector(raw, flag string, valid []string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	allowed := make(map[string]bool, len(valid))
	for _, value := range valid {
		allowed[value] = true
	}
	seen := make(map[string]bool)
	var values []string
	for _, value := range strings.Split(raw, ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !allowed[value] {
			return nil, fmt.Errorf("invalid --%s value %q; valid values: %s", flag, value, strings.Join(valid, ", "))
		}
		if !seen[value] {
			seen[value] = true
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("--%s requires at least one value; valid values: %s", flag, strings.Join(valid, ", "))
	}
	return values, nil
}

func categoryStrings(values []types.Category) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func toolStrings(values []types.Tool) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func init() {
	cleanCmd.Flags().StringVarP(&cleanAge, "age", "a", "7d", "Minimum idle age (7d, 2w, 1mo, 1y, 24h)")
	cleanCmd.Flags().StringVarP(
		&cleanCategory,
		"category",
		"c",
		"",
		"Comma-separated categories ("+strings.Join(categoryStrings(validCleanCategories), ",")+")",
	)
	cleanCmd.Flags().StringVarP(
		&cleanTools,
		"tool",
		"t",
		"",
		"Comma-separated tools ("+strings.Join(toolStrings(validCleanTools), ",")+")",
	)
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview without deleting")
	cleanCmd.Flags().BoolVar(&cleanJSON, "json", false, "Emit a machine-readable cleanup plan or classic execution receipt")
	cleanCmd.Flags().BoolVar(&cleanIncludePaths, "include-paths", false, "Include paths and cleanup commands in JSON output")
	cleanCmd.Flags().BoolVarP(&cleanInteractive, "interactive", "i", false, "Confirm each deletion")
	cleanCmd.Flags().BoolVar(&cleanRisky, "risky", false, "Include risky categories (ai-logs)")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Skip confirmation prompt")
	cleanCmd.Flags().BoolVar(&cleanGuide, "guide", false, "Guided worktree cleanup review")
	cleanCmd.Flags().BoolVar(&cleanNoGuide, "no-guide", false, "Use classic cleanup even when guided worktree review is available")
	cleanCmd.Flags().BoolVar(&cleanStrip, "strip", false, "Strip regenerable subtrees from protected worktrees instead of deleting units")
	cleanCmd.Flags().BoolVar(&cleanAPFSSnapshots, "apfs-snapshots", false, "Opt-in APFS local-snapshot thinning (macOS only; never default)")
	cleanCmd.Flags().BoolVar(&cleanPressure, "pressure", false, "Select official regenerable caches younger than --age (also auto when the home volume is critical, ≥95% used)")
	cleanCmd.Flags().StringArrayVar(&cleanRoots, "root", nil, "Scan root under $HOME (repeatable)")
	cleanCmd.Flags().StringArrayVar(&cleanExcludes, "exclude", nil, "Exclude a path or glob pattern under scan roots from discovery (repeatable)")
	cleanCmd.Flags().BoolVar(&cleanIncludeActiveWorktrees, "include-active-worktrees", false, "Include active worktrees in cleanup candidates")
	cleanCmd.Flags().StringVar(
		&cleanAgentStateGrace,
		"agent-state-grace",
		"24h",
		"Minimum idle age before an orphaned agent-state entry is selected by default (0 disables)",
	)
	cleanCmd.Flags().StringVar(
		&cleanReceiptFile,
		"receipt-file",
		"",
		"Write a machine-readable execution receipt to this path",
	)
}

func confirmCleanExecution() bool {
	fmt.Print("Proceed? [y/N]: ")
	var response string
	if _, err := fmt.Scanln(&response); err != nil {
		fmt.Println("No confirmation received; rerun with --dry-run to review or --force to delete selected targets.")
		return false
	}
	if response != "y" && response != "Y" {
		fmt.Println("Aborted.")
		return false
	}
	return true
}

type cleanPlanMode string

const (
	cleanPlanModeDelete cleanPlanMode = "delete"
	cleanPlanModeDryRun cleanPlanMode = "dry-run"
)

func parseAge(s string) (time.Duration, error) {
	units := []struct {
		suffix string
		unit   time.Duration
	}{
		{suffix: "mo", unit: 30 * 24 * time.Hour},
		{suffix: "y", unit: 365 * 24 * time.Hour},
		{suffix: "w", unit: 7 * 24 * time.Hour},
		{suffix: "d", unit: 24 * time.Hour},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64)
			if err != nil {
				return 0, err
			}
			return time.Duration(n * float64(u.unit)), nil
		}
	}
	return time.ParseDuration(s)
}

func printCleanHeader(roots []string) {
	fmt.Println("clean")
	fmt.Printf("  roots  %s\n\n", strings.Join(displayRoots(roots), ", "))
}

func shortDurationString(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

func printCleanCandidateSummary(targets []types.DebrisInfo) {
	var totalSize int64
	for _, w := range targets {
		totalSize += w.Size
	}
	fmt.Printf("  matched  %d %s   %s\n\n",
		len(targets), candidateNoun(len(targets)), cleaner.FormatSize(totalSize))
}

func candidateNoun(count int) string {
	if count == 1 {
		return "candidate"
	}
	return "candidates"
}
