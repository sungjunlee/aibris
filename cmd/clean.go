package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
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

func mergeGuidedPreviewWithClassicTargets(guided, classic []types.DebrisInfo) ([]types.DebrisInfo, []types.DebrisInfo) {
	guidedTargets := cleaner.NormalizeTargets(guided)
	guidedPaths := make([]string, 0, len(guidedTargets))
	for _, target := range guidedTargets {
		if path, ok := cleaner.TargetPathKey(target.Path); ok {
			guidedPaths = append(guidedPaths, path)
		}
	}

	classicTargets := make([]types.DebrisInfo, 0, len(classic))
	for _, target := range classic {
		path, ok := cleaner.TargetPathKey(target.Path)
		if !ok {
			continue
		}
		overlapsGuided := false
		for _, guidedPath := range guidedPaths {
			if path == guidedPath || cleaner.PathContains(guidedPath, path) || cleaner.PathContains(path, guidedPath) {
				overlapsGuided = true
				break
			}
		}
		if !overlapsGuided {
			classicTargets = append(classicTargets, target)
		}
	}
	classicTargets = cleaner.NormalizeTargets(classicTargets)

	auditTargets := make([]types.DebrisInfo, 0, len(guidedTargets)+len(classicTargets))
	auditTargets = append(auditTargets, guidedTargets...)
	auditTargets = append(auditTargets, classicTargets...)
	return classicTargets, auditTargets
}

func mergeCleanupOverlapComponents(
	preferred []cleanupOverlapComponent,
	remaining []cleanupOverlapComponent,
) []cleanupOverlapComponent {
	merged := append([]cleanupOverlapComponent(nil), preferred...)
	for _, component := range remaining {
		overlapsPreferred := false
		for _, existing := range preferred {
			if _, overlaps := cleanupLogicalRelation(
				existing.CanonicalPath,
				component.CanonicalPath,
			); overlaps {
				overlapsPreferred = true
				break
			}
		}
		if !overlapsPreferred {
			merged = append(merged, component)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].CanonicalPath == merged[j].CanonicalPath {
			return cleaner.TargetStableKey(merged[i].Owner) <
				cleaner.TargetStableKey(merged[j].Owner)
		}
		return merged[i].CanonicalPath < merged[j].CanonicalPath
	})
	return merged
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

func applyGuidedCleanDefaults(cmd *cobra.Command, age time.Duration) time.Duration {
	if cleanCategory == "" {
		cleanCategory = string(types.CategoryWorktree)
	}
	// --guide no longer narrows to Codex. Guided review is built on Git
	// evidence, which every tool's worktree carries, so leaving the tool
	// filter empty admits them all; --tool still narrows when asked.
	return guidedCleanAge(cmd, age)
}

func guidedCleanAge(cmd *cobra.Command, age time.Duration) time.Duration {
	if !cmd.Flags().Changed("age") {
		return DefaultMinIdleAge
	}
	return age
}

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

// shouldRelaxCacheAge reports whether official regenerable caches may ignore
// --age. --pressure always enables it for every official cache. Automatic
// critical-volume mode only relaxes caches on the home volume.
func shouldRelaxCacheAge(explicit bool) (bool, string) {
	if explicit {
		return true, ""
	}
	return scanreport.AutoRelaxCacheAge()
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

func scanForClean(ctx context.Context, roots, excludes []string, explicit bool) (*types.ScanResult, scanSource, error) {
	return loadLastScanSession(ctx, roots, excludes, cleanScanSelector(), explicit, true)
}

func scanForCleanQuiet(ctx context.Context, roots, excludes []string, explicit bool) (*types.ScanResult, scanSource, error) {
	return loadLastScanSession(ctx, roots, excludes, cleanScanSelector(), explicit, false)
}

func cleanScanSelector() string {
	if cleanStrip {
		return "strip"
	}
	if cleanPressure {
		return "pressure"
	}
	return "delete"
}

var errIncompleteCleanupScan = errors.New("cleanup requires a complete scan")

func requireCompleteScan(result *types.ScanResult) error {
	if result == nil || !result.Partial() {
		return nil
	}
	providers := make([]string, 0, len(result.ProviderErrors))
	for _, providerErr := range result.ProviderErrors {
		providers = append(providers, string(providerErr.Tool))
	}
	return fmt.Errorf("%w; failed providers: %s", errIncompleteCleanupScan, strings.Join(providers, ", "))
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

func filterTargetsWithoutScanEvidence(targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	filtered := targets[:0]
	protections := make(map[string]cleanAuditReason)
	for _, target := range targets {
		if target.ScanPathEvidenceRequired && target.ScanPathIdentity == "" {
			protections[cleanAuditItemKey(target)] = cleanReasonScanEvidenceUnavailable
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered, protections
}

type worktreeGitInspector func(context.Context, string) worktreeGitSafety

func filterGitUnsafeActiveWorktreeTargets(ctx context.Context, targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	return filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx, targets, inspectActiveWorktreeCleanupSafety)
}

func filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx context.Context, targets []types.DebrisInfo, inspector worktreeGitInspector) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	protections := make(map[string]cleanAuditReason)
	filtered := targets[:0]
	for _, target := range targets {
		if target.Category != types.CategoryWorktree || target.Status != types.WorktreeActive {
			filtered = append(filtered, target)
			continue
		}

		safety := inspector(ctx, target.Path)
		if !safety.Protected {
			filtered = append(filtered, target)
			continue
		}

		reason := gitProtectionGitStatusUnavailable
		if len(safety.ProtectionReasons) > 0 {
			reason = strings.Join(safety.ProtectionReasons, ", ")
		}
		protections[cleanAuditItemKey(target)] = cleanAuditReason(reason)
	}
	return filtered, protections
}

func interactiveClean(ctx context.Context, targets []preparedCleanTarget) (cleanExecutionReceipt, error) {
	return interactiveCleanWithValidation(ctx, targets, nil)
}

// interactiveCleanSkipOutcome reports a prepared target the confirmation loop
// left without an execution unit. Declined is true when the operator answered
// the prompt and refused, and false when the confirmation stream ended before
// an answer arrived. Observers are informational: they cannot affect cleanup
// safety, execution, or the printed confirmation.
type interactiveCleanSkipOutcome struct {
	Target   preparedCleanTarget
	Declined bool
}

type interactiveCleanSkipObserver func(interactiveCleanSkipOutcome)

func interactiveCleanWithValidation(
	ctx context.Context,
	targets []preparedCleanTarget,
	validate func(context.Context) error,
) (cleanExecutionReceipt, error) {
	return interactiveCleanWithValidationAndObserver(ctx, targets, validate, nil)
}

// reportUnansweredCleanTargets hands the targets whose confirmation never
// arrived to an optional observer. It prints nothing, so the confirmation loop
// reads identically with and without an observer.
func reportUnansweredCleanTargets(
	observer interactiveCleanSkipObserver,
	targets []preparedCleanTarget,
) {
	if observer == nil {
		return
	}
	for _, target := range targets {
		observer(interactiveCleanSkipOutcome{Target: target})
	}
}

func interactiveCleanWithValidationAndObserver(
	ctx context.Context,
	targets []preparedCleanTarget,
	validate func(context.Context) error,
	observer interactiveCleanSkipObserver,
) (cleanExecutionReceipt, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return cleanExecutionReceipt{}, fmt.Errorf("getting home dir: %w", err)
	}
	displayHome := resolvedDisplayHome(home)

	var result cleanExecutionReceipt
	var errs []error
	scanner := bufio.NewScanner(os.Stdin)
	for i, target := range targets {
		w := target.Item
		if !cleaner.IsSafeTarget(home, w) {
			err := fmt.Errorf("unsafe path %q rejected", w.Path)
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
			result.Units = append(result.Units, failedPreparedCleanUnitReceipt(target, err))
			errs = append(errs, err)
			continue
		}
		fmt.Println()
		printCleanTarget(w, displayHome)
		fmt.Print("Remove? [y/N]: ")
		if !scanner.Scan() {
			reportUnansweredCleanTargets(observer, targets[i:])
			break
		}
		response := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if response == "y" || response == "yes" {
			if validate != nil {
				if err := validate(ctx); err != nil {
					for _, remaining := range targets[i:] {
						result.Units = append(result.Units, failedPreparedCleanUnitReceipt(remaining, err))
					}
					return result, err
				}
			}
			receipt, err := executePreparedCleanTargets(ctx, []preparedCleanTarget{target}, defaultActiveWorktreeExecutionOptions())
			result.Units = append(result.Units, receipt.Units...)
			result.FreedBytes += receipt.FreedBytes
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				errs = append(errs, err)
				continue
			}
		} else {
			fmt.Printf("  skipped\n")
			if observer != nil {
				observer(interactiveCleanSkipOutcome{Target: target, Declined: true})
			}
		}
	}
	return result, errors.Join(errs...)
}

func printCleanPlan(targets []types.DebrisInfo, mode cleanPlanMode) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	} else {
		home = resolvedDisplayHome(home)
	}
	var totalSize int64
	for _, w := range targets {
		totalSize += w.Size
	}

	fmt.Println("clean plan")
	fmt.Printf("  mode     %s\n", mode)
	fmt.Printf("  targets  %d %s   %s\n", len(targets), itemNoun(len(targets)), cleaner.FormatSize(totalSize))
	fmt.Println()
	fmt.Println("targets")
	fmt.Printf("  %8s  %-13s %-12s %-18s %-14s %-12s %s\n",
		"size", "category", "name", "project", "age/status", "action", "reason")
	for _, w := range displayCleanPlanTargets(targets, mode) {
		printCleanTarget(w, home)
	}
	fmt.Println()
}

func displayCleanPlanTargets(targets []types.DebrisInfo, mode cleanPlanMode) []types.DebrisInfo {
	if mode != cleanPlanModeDryRun {
		return targets
	}
	displayed := append([]types.DebrisInfo(nil), targets...)
	sort.SliceStable(displayed, func(i, j int) bool {
		if displayed[i].Size != displayed[j].Size {
			return displayed[i].Size > displayed[j].Size
		}
		return cleaner.TargetStableKey(displayed[i]) < cleaner.TargetStableKey(displayed[j])
	})
	return displayed
}

func printCleanPlanWithComponents(
	targets []types.DebrisInfo,
	components []cleanupOverlapComponent,
	mode cleanPlanMode,
) {
	printCleanPlan(targets, mode)
	targetKeys := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetKeys[cleaner.TargetStableKey(target)] = true
	}
	printedHeader := false
	for _, component := range components {
		if !targetKeys[cleaner.TargetStableKey(component.Owner)] ||
			!cleanupComponentHasLineage(component) {
			continue
		}
		if !printedHeader {
			fmt.Println("overlap lineage")
			printedHeader = true
		}
		printCleanupComponentLineage(component, "  ")
	}
	if printedHeader {
		fmt.Println()
	}
}

func cleanupComponentHasLineage(component cleanupOverlapComponent) bool {
	if len(component.LogicalRows) > 1 || len(component.Obligations) > 0 {
		return true
	}
	for _, row := range component.LogicalRows {
		if row.L1Reason != "" {
			return true
		}
	}
	return false
}

func printCleanupComponentLineage(
	component cleanupOverlapComponent,
	indent string,
) {
	fmt.Printf("%sowner     %s   %s\n",
		indent,
		itemName(component.Owner),
		cleaner.FormatSize(component.Owner.Size))
	for _, row := range component.LogicalRows {
		classification := ""
		if row.Item.Classification != "" {
			classification = " classification=" + string(row.Item.Classification)
		}
		displayPath := cleanupLogicalDisplayPath(component, row)
		fmt.Printf("%sevidence  %-19s %-13s %-12s%s %s\n",
			indent+"  ",
			row.Relation,
			row.Item.Category,
			row.Item.Tool,
			classification,
			displayPath)
		if row.PolicyReason != "" {
			fmt.Printf("%s  policy   %s\n", indent+"  ", row.PolicyReason)
		}
		if row.L1Reason != "" {
			fmt.Printf("%s  overlap  %s\n", indent+"  ", row.L1Reason)
		}
	}
}

func cleanupLogicalDisplayPath(
	component cleanupOverlapComponent,
	row cleanupOverlapLogicalRow,
) string {
	switch row.Relation {
	case cleanupOverlapOwner:
		return "[owner target]"
	case cleanupOverlapExact:
		return fmt.Sprintf("[exact discovery %d]", row.DiscoveryOrdinal)
	case cleanupOverlapDescendant:
		rel, err := filepath.Rel(component.CanonicalPath, row.CanonicalPath)
		if err == nil {
			return "." + string(filepath.Separator) + rel
		}
	case cleanupOverlapAncestor:
		return "[containing protected discovery]"
	}
	return row.Item.Path
}

func printCleanTarget(w types.DebrisInfo, home string) {
	fmt.Println(cleanPlanLine(w))
	if home != "" {
		fmt.Printf("    %s\n", displayHomePath(home, w.Path))
	} else {
		fmt.Printf("    %s\n", w.Path)
	}
	if cleanupKind(w) == types.CleanupCommand && len(w.CleanupCommand) > 0 {
		fmt.Printf("    command: %s\n", strings.Join(w.CleanupCommand, " "))
	}
}

func cleanPlanLine(w types.DebrisInfo) string {
	return fmt.Sprintf("  %8s  %-13s %-12s %-18s %-14s %-12s %s",
		cleaner.FormatSize(w.Size),
		w.Category,
		itemName(w),
		itemProject(w),
		itemAgeAndStatus(w),
		cleanAction(w),
		cleanTargetReason(w))
}

func cleanAction(w types.DebrisInfo) string {
	if cleanupKind(w) == types.CleanupCommand && len(w.CleanupCommand) > 0 {
		return string(types.CleanupCommand)
	}
	return string(types.CleanupRemovePath)
}

func resolvedDisplayHome(home string) string {
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		return resolved
	}
	return home
}
