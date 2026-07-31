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
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	cleanAge                    string
	cleanCategory               string
	cleanTools                  string
	cleanDryRun                 bool
	cleanInteractive            bool
	cleanRisky                  bool
	cleanForce                  bool
	cleanGuide                  bool
	cleanNoGuide                bool
	cleanRoots                  []string
	cleanIncludeActiveWorktrees bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up old AI tool debris",
	Long: `Clean up old AI tool debris.

With no classic cleanup filters, clean uses guided worktree review by default when useful.
After guided worktree review, clean continues with the classic all-category audit.
Use --no-guide, or pass an explicit classic selector such as --category, --tool,
--risky, --force, --include-active-worktrees, or --interactive to keep the
classic cleanup audit and executor route.

Across both routes, selected targets enter the cleanup plan, reviewable targets
require explicit selection, and protected targets never enter the plan. Guided
review displays protected targets as locked rows. --guide defaults only an
omitted category to worktree; an explicit --tool narrows the review normally.
Tools without a registered activity source are reviewable but never initially
selected or automatically recommended.`,
	Run: func(cmd *cobra.Command, args []string) {
		if cleanGuide && cleanNoGuide {
			fmt.Fprintln(os.Stderr, "error: cannot use --guide with --no-guide")
			os.Exit(1)
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

		result, source, err := scanForClean(ctx, roots)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		refreshCleanupInventoryMetadata(result.Worktrees)
		overlapSafety, err := newDefaultCleanupOverlapSafetyRuntime(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: preparing overlap safety: %v\n", err)
			os.Exit(1)
		}

		var guidedState guidedCleanState
		usefulGuidedWorktreeReview := false
		if shouldPrepareGuidedClean(cmd) {
			usefulGuidedWorktreeReview = hasGuidedWorktreeCleanupPressure(ctx, result.Worktrees)
		}
		if cleanGuide || usefulGuidedWorktreeReview {
			guidedState, err = buildGuidedCleanState(
				ctx,
				result,
				source,
				categories,
				tools,
				guidedAge,
				"",
			)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: preparing guided cleanup: %v\n", err)
				os.Exit(1)
			}
		}
		experience, reason, err := chooseCleanExperience(cleanExperienceInputFromCommand(cmd, usefulGuidedWorktreeReview))
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
		}

		var guidedPreviewTargets []types.DebrisInfo
		var guidedComponents []cleanupOverlapComponent
		var guidedSafetyProtections map[string]cleanAuditReason
		guidedHadSelection := false
		if experience == cleanExperienceGuided {
			guidedOpts := opts
			guidedOpts.Age = guidedAge
			guidedState.Reason = reason
			guidedResult, err := runGuidedWorktreeClean(ctx, guidedOpts, guidedState, overlapSafety)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			if guidedResult.Aborted {
				return
			}
			guidedPreviewTargets = guidedResult.PreviewTargets
			guidedComponents = guidedResult.Components
			guidedSafetyProtections = guidedResult.SafetyProtections
			guidedHadSelection = guidedResult.HadSelection
			opts.IncludeActiveWorktrees = false
		}

		targets := cleaner.Filter(result.Worktrees, opts)
		targets, physicalOwnerProtections := applyPhysicalWorktreeOwnerSafety(
			result.Worktrees,
			targets,
			opts.IncludeActiveWorktrees,
		)
		targets = filterExistingTargets(targets)
		targets, scanEvidenceProtections := filterTargetsWithoutScanEvidence(targets)
		targets = normalizeCleanTargets(targets)
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
		auditTargets := targets
		if len(guidedPreviewTargets) > 0 {
			targets, auditTargets = mergeGuidedPreviewWithClassicTargets(guidedPreviewTargets, targets)
		}
		overlapSelection.Targets = targets
		auditProtections := mergeCleanAuditProtections(
			classicProtections,
			overlapSelection.Protections,
			guidedSafetyProtections,
		)
		auditComponents := mergeCleanupOverlapComponents(
			guidedComponents,
			overlapSelection.Components,
		)
		audit := buildPhysicalCleanAudit(
			result.Worktrees,
			auditComponents,
			auditTargets,
			opts,
			len(scanner.DefaultScanner.Providers),
			source,
			auditProtections,
		)
		printCleanAudit(audit, opts)
		printCleanCandidateSummary(targets)

		if len(targets) == 0 {
			if guidedHadSelection {
				fmt.Println("No additional classic items to clean.")
			} else {
				fmt.Println("No items to clean.")
			}
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
	},
}

func mergeGuidedPreviewWithClassicTargets(guided, classic []types.DebrisInfo) ([]types.DebrisInfo, []types.DebrisInfo) {
	guidedTargets := normalizeCleanTargets(guided)
	guidedPaths := make([]string, 0, len(guidedTargets))
	for _, target := range guidedTargets {
		if path, ok := cleanTargetPathKey(target.Path); ok {
			guidedPaths = append(guidedPaths, path)
		}
	}

	classicTargets := make([]types.DebrisInfo, 0, len(classic))
	for _, target := range classic {
		path, ok := cleanTargetPathKey(target.Path)
		if !ok {
			continue
		}
		overlapsGuided := false
		for _, guidedPath := range guidedPaths {
			if path == guidedPath || cleanTargetContains(guidedPath, path) || cleanTargetContains(path, guidedPath) {
				overlapsGuided = true
				break
			}
		}
		if !overlapsGuided {
			classicTargets = append(classicTargets, target)
		}
	}
	classicTargets = normalizeCleanTargets(classicTargets)

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
			return cleanTargetStableKey(merged[i].Owner) <
				cleanTargetStableKey(merged[j].Owner)
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
	cleanCmd.Flags().BoolVarP(&cleanInteractive, "interactive", "i", false, "Confirm each deletion")
	cleanCmd.Flags().BoolVar(&cleanRisky, "risky", false, "Include risky categories (ai-logs)")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Skip confirmation prompt")
	cleanCmd.Flags().BoolVar(&cleanGuide, "guide", false, "Guided worktree cleanup review (defaults only omitted category to worktree)")
	cleanCmd.Flags().BoolVar(&cleanNoGuide, "no-guide", false, "Use classic cleanup even when guided worktree review is available")
	cleanCmd.Flags().StringArrayVar(&cleanRoots, "root", nil, "Scan root under $HOME (repeatable)")
	cleanCmd.Flags().BoolVar(&cleanIncludeActiveWorktrees, "include-active-worktrees", false, "Include active worktrees in cleanup candidates")
}

func applyGuidedCleanDefaults(cmd *cobra.Command, age time.Duration) time.Duration {
	if cleanCategory == "" {
		cleanCategory = string(types.CategoryWorktree)
	}
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
	cleanExperienceGuided  cleanExperience = "guided-worktree"

	guidedWorktreeCleanupPressureMinSize       int64 = 256 * 1024 * 1024
	guidedWorktreeCleanupPressureUnitThreshold       = 3

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
	UsefulGuidedWorktreeReview    bool
}

func cleanExperienceInputFromCommand(cmd *cobra.Command, usefulGuidedWorktreeReview bool) cleanExperienceInput {
	return cleanExperienceInput{
		Guide:                         cleanGuide,
		NoGuide:                       cleanNoGuide,
		CategoryChanged:               cmd.Flags().Changed("category"),
		ToolChanged:                   cmd.Flags().Changed("tool"),
		RiskyChanged:                  cmd.Flags().Changed("risky"),
		ForceChanged:                  cmd.Flags().Changed("force"),
		IncludeActiveWorktreesChanged: cmd.Flags().Changed("include-active-worktrees"),
		InteractiveChanged:            cmd.Flags().Changed("interactive"),
		UsefulGuidedWorktreeReview:    usefulGuidedWorktreeReview,
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
	if input.UsefulGuidedWorktreeReview {
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

func hasGuidedWorktreeCleanupPressure(ctx context.Context, items []types.DebrisInfo) bool {
	unitCount, totalSize := guidedWorktreeCleanupPressure(ctx, items)
	return isGuidedWorktreeCleanupPressureValuable(unitCount, totalSize)
}

func isGuidedWorktreeCleanupPressureValuable(unitCount int, totalSize int64) bool {
	return unitCount > 0 && (totalSize >= guidedWorktreeCleanupPressureMinSize || unitCount >= guidedWorktreeCleanupPressureUnitThreshold)
}

func guidedWorktreeCleanupPressure(ctx context.Context, items []types.DebrisInfo) (int, int64) {
	candidates := guidedActiveWorktrees(items, nil, nil)

	units, err := buildWorktreeCleanupUnits(ctx, candidates)
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

func scanForClean(ctx context.Context, roots []string) (*types.ScanResult, scanSource, error) {
	if result, age, ok := readFreshLastScanCache(roots); ok {
		if err := requireCompleteScan(result); err != nil {
			return nil, scanSource{}, err
		}
		return result, scanSource{Kind: scanSourceCached, Age: age}, nil
	}

	progress := newScanProgressPrinter(os.Stdout)
	result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{
		Roots:      roots,
		OnProgress: progress.Handle,
	})
	progress.Stop()
	if err != nil {
		return nil, scanSource{}, err
	}
	if err := requireCompleteScan(result); err != nil {
		return nil, scanSource{}, err
	}
	writeLastScanCache(roots, result)
	return result, scanSource{Kind: scanSourceLive}, nil
}

func requireCompleteScan(result *types.ScanResult) error {
	if result == nil || !result.Partial() {
		return nil
	}
	providers := make([]string, 0, len(result.ProviderErrors))
	for _, providerErr := range result.ProviderErrors {
		providers = append(providers, string(providerErr.Tool))
	}
	return fmt.Errorf("cleanup requires a complete scan; failed providers: %s", strings.Join(providers, ", "))
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

func filterExistingTargets(targets []types.DebrisInfo) []types.DebrisInfo {
	filtered := targets[:0]
	for _, target := range targets {
		if _, err := os.Lstat(target.Path); err == nil {
			filtered = append(filtered, target)
		}
	}
	return filtered
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

// applyPhysicalWorktreeOwnerSafety evaluates worktree eligibility at the
// physical owner boundary. Scanner compatibility rows may disagree on status
// while sharing one mutation path, so no orphaned row may independently
// authorize removal of an owner that also has an active row.
func applyPhysicalWorktreeOwnerSafety(
	inventory []types.DebrisInfo,
	targets []types.DebrisInfo,
	includeActive bool,
) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	activeOwners := make(map[string]bool)
	for _, item := range inventory {
		if item.Category != types.CategoryWorktree ||
			item.Status != types.WorktreeActive {
			continue
		}
		if path, ok := cleanTargetPathKey(item.Path); ok {
			activeOwners[path] = true
		}
	}

	protections := make(map[string]cleanAuditReason)
	if !includeActive {
		for _, item := range inventory {
			if item.Category != types.CategoryWorktree {
				continue
			}
			path, ok := cleanTargetPathKey(item.Path)
			if ok && activeOwners[path] {
				protections[cleanAuditItemKey(item)] = cleanReasonActiveWorktree
			}
		}
	}

	filtered := make([]types.DebrisInfo, 0, len(targets))
	for _, target := range targets {
		if target.Category != types.CategoryWorktree {
			filtered = append(filtered, target)
			continue
		}
		path, ok := cleanTargetPathKey(target.Path)
		if !ok || !activeOwners[path] {
			filtered = append(filtered, target)
			continue
		}
		if !includeActive {
			continue
		}
		// Preserve the selected raw mutation path and its physical byte
		// estimate, but force the owner through the active/Git-aware route.
		target.Status = types.WorktreeActive
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

type normalizedCleanTarget struct {
	item     types.DebrisInfo
	path     string
	depth    int
	index    int
	rawSizes map[string]int64
}

func normalizeCleanTargets(targets []types.DebrisInfo) []types.DebrisInfo {
	byPath := make(map[string]normalizedCleanTarget, len(targets))
	for i, target := range targets {
		path, ok := cleanTargetPathKey(target.Path)
		if !ok {
			continue
		}
		candidate := normalizedCleanTarget{
			item:     target,
			path:     path,
			depth:    cleanTargetPathDepth(path),
			index:    i,
			rawSizes: map[string]int64{cleanTargetRawPathKey(target.Path): target.Size},
		}
		existing, exists := byPath[path]
		if !exists {
			byPath[path] = candidate
			continue
		}
		// Canonical aliases share containment identity, but they do not share a
		// raw mutation target. Keep byte estimates scoped to the selected raw
		// path so deleting a symlink cannot inherit its referent's accounting.
		rawPath := cleanTargetRawPathKey(candidate.item.Path)
		if candidate.item.Size > existing.rawSizes[rawPath] {
			existing.rawSizes[rawPath] = candidate.item.Size
		}
		hasActiveWorktree := isActiveWorktreeTarget(existing.item) ||
			isActiveWorktreeTarget(candidate.item)
		if preferCleanTargetForCanonical(candidate.item, existing.item, path) {
			existing.item = candidate.item
		}
		if hasActiveWorktree && existing.item.Category == types.CategoryWorktree {
			// Canonical aliases still prefer the direct raw mutation owner, but
			// a mixed active/orphaned owner must retain active execution
			// semantics regardless of which raw row supplied that owner.
			existing.item.Status = types.WorktreeActive
		}
		existing.item.Size = existing.rawSizes[cleanTargetRawPathKey(existing.item.Path)]
		if candidate.index < existing.index {
			existing.index = candidate.index
		}
		byPath[path] = existing
	}

	candidates := make([]normalizedCleanTarget, 0, len(byPath))
	for _, candidate := range byPath {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.depth == right.depth {
			return left.path < right.path
		}
		return left.depth < right.depth
	})

	kept := make([]normalizedCleanTarget, 0, len(candidates))
	for _, candidate := range candidates {
		nested := false
		for _, parent := range kept {
			if cleanTargetContains(parent.path, candidate.path) {
				nested = true
				break
			}
		}
		if !nested {
			kept = append(kept, candidate)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].index == kept[j].index {
			return kept[i].path < kept[j].path
		}
		return kept[i].index < kept[j].index
	})

	normalized := make([]types.DebrisInfo, 0, len(kept))
	for _, target := range kept {
		normalized = append(normalized, target.item)
	}
	return normalized
}

func cleanTargetPathKey(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = filepath.Clean(resolved)
	}
	return clean, true
}

func cleanTargetRawPathKey(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

func preferCleanTargetForCanonical(left, right types.DebrisInfo, canonicalPath string) bool {
	leftIsSymlink := cleanTargetPathIsSymlink(left.Path)
	rightIsSymlink := cleanTargetPathIsSymlink(right.Path)
	if leftIsSymlink != rightIsSymlink {
		return !leftIsSymlink
	}
	leftIsCanonical := cleanTargetRawPathKey(left.Path) == canonicalPath
	rightIsCanonical := cleanTargetRawPathKey(right.Path) == canonicalPath
	if leftIsCanonical != rightIsCanonical {
		return leftIsCanonical
	}
	return preferCleanTarget(left, right)
}

func cleanTargetPathIsSymlink(path string) bool {
	info, err := os.Lstat(cleanTargetRawPathKey(path))
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func preferCleanTarget(left, right types.DebrisInfo) bool {
	leftRank := cleanTargetRank(left)
	rightRank := cleanTargetRank(right)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	return cleanTargetStableKey(left) < cleanTargetStableKey(right)
}

func cleanTargetRank(target types.DebrisInfo) int {
	if target.Category == types.CategoryWorktree {
		return 0
	}
	if cleanupKind(target) == types.CleanupRemovePath {
		return 1
	}
	return 2
}

func cleanTargetStableKey(target types.DebrisInfo) string {
	return strings.Join([]string{
		string(target.Category),
		string(target.Tool),
		target.ID,
		target.Project,
		target.Source,
		string(target.Status),
		string(cleanupKind(target)),
		strings.Join(target.CleanupCommand, "\x00"),
		target.Path,
	}, "\x00")
}

func cleanTargetPathDepth(path string) int {
	volume := filepath.VolumeName(path)
	trimmed := strings.Trim(strings.TrimPrefix(path, volume), string(filepath.Separator))
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, string(filepath.Separator)) + 1
}

func cleanTargetContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func interactiveClean(ctx context.Context, targets []preparedCleanTarget) (cleanExecutionReceipt, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return cleanExecutionReceipt{}, fmt.Errorf("getting home dir: %w", err)
	}
	displayHome := resolvedDisplayHome(home)

	var result cleanExecutionReceipt
	var errs []error
	scanner := bufio.NewScanner(os.Stdin)
	for _, target := range targets {
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
			break
		}
		response := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if response == "y" || response == "yes" {
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
	for _, w := range targets {
		printCleanTarget(w, home)
	}
	fmt.Println()
}

func printCleanPlanWithComponents(
	targets []types.DebrisInfo,
	components []cleanupOverlapComponent,
	mode cleanPlanMode,
) {
	printCleanPlan(targets, mode)
	targetKeys := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetKeys[cleanTargetStableKey(target)] = true
	}
	printedHeader := false
	for _, component := range components {
		if !targetKeys[cleanTargetStableKey(component.Owner)] ||
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
