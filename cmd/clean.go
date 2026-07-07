package cmd

import (
	"bufio"
	"context"
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
	cleanRoots                  []string
	cleanIncludeActiveWorktrees bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up old AI tool debris",
	Run: func(cmd *cobra.Command, args []string) {
		age, err := parseAge(cleanAge)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid age '%s': expected duration like 7d, 2w, 1mo, 1y, or 24h\n", cleanAge)
			os.Exit(1)
		}

		if age <= 0 {
			fmt.Fprintf(os.Stderr, "error: --age must be positive (got %s)\n", cleanAge)
			os.Exit(1)
		}
		if age < time.Hour {
			fmt.Fprintf(os.Stderr, "Warning: --age %s will match ALL items including active ones.\n", cleanAge)
		}
		if cleanGuide {
			if cleanCategory == "" {
				cleanCategory = string(types.CategoryWorktree)
			}
			if cleanTools == "" {
				cleanTools = string(types.ToolCodex)
			}
			if cleanAge == "7d" {
				age = guidedCodexDefaultAge
			}
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

		var categories []types.Category
		if cleanCategory != "" {
			for _, c := range strings.Split(cleanCategory, ",") {
				c = strings.TrimSpace(c)
				if c != "" {
					categories = append(categories, types.Category(c))
				}
			}
		}

		var tools []types.Tool
		if cleanTools != "" {
			for _, t := range strings.Split(cleanTools, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tools = append(tools, types.Tool(t))
				}
			}
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

		if cleanGuide {
			if err := runGuidedCodexClean(ctx, result, source, opts); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		targets := cleaner.Filter(result.Worktrees, opts)
		targets = filterExistingTargets(targets)
		targets = normalizeCleanTargets(targets)
		targets, gitSafetyProtections := filterGitUnsafeActiveWorktreeTargets(ctx, targets)
		audit := buildCleanAudit(result.Worktrees, targets, opts, len(scanner.DefaultScanner.Providers), source, gitSafetyProtections)
		printCleanAudit(audit, opts)
		printCleanCandidateSummary(targets)

		if len(targets) == 0 {
			fmt.Println("No items to clean.")
			return
		}

		if opts.DryRun {
			printCleanPlan(targets, cleanPlanModeDryRun)
			fmt.Println("[DRY-RUN] No files were removed.")
			return
		}

		if opts.Interactive {
			total := interactiveClean(targets)
			printCleanupReceipt(len(targets), total, audit)
			return
		}

		if !opts.Force {
			printCleanPlan(targets, cleanPlanModeDelete)
			fmt.Print("Proceed? [y/N]: ")
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Aborted.")
				return
			}
		}

		total, err := cleaner.Execute(targets)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
		}
		printCleanupReceipt(len(targets), total, audit)
	},
}

func init() {
	cleanCmd.Flags().StringVarP(&cleanAge, "age", "a", "7d", "Max age (7d, 2w, 1mo, 1y, 24h)")
	cleanCmd.Flags().StringVarP(&cleanCategory, "category", "c", "", "Comma-separated categories (worktree,node_modules,build-cache,other-cache,ai-logs)")
	cleanCmd.Flags().StringVarP(&cleanTools, "tool", "t", "", "Comma-separated tools (codex,claude,cursor,windsurf,node_modules,build-cache,pip-cache,ai-logs)")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview without deleting")
	cleanCmd.Flags().BoolVarP(&cleanInteractive, "interactive", "i", false, "Confirm each deletion")
	cleanCmd.Flags().BoolVar(&cleanRisky, "risky", false, "Include risky categories (ai-logs)")
	cleanCmd.Flags().BoolVarP(&cleanForce, "force", "f", false, "Skip confirmation prompt")
	cleanCmd.Flags().BoolVar(&cleanGuide, "guide", false, "Guided Codex worktree cleanup review")
	cleanCmd.Flags().StringArrayVar(&cleanRoots, "root", nil, "Scan root under $HOME (repeatable)")
	cleanCmd.Flags().BoolVar(&cleanIncludeActiveWorktrees, "include-active-worktrees", false, "Include active worktrees in cleanup candidates")
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
	writeLastScanCache(roots, result)
	return result, scanSource{Kind: scanSourceLive}, nil
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
		if _, err := os.Stat(target.Path); err == nil {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

type worktreeGitInspector func(context.Context, string) worktreeGitSafety

func filterGitUnsafeActiveWorktreeTargets(ctx context.Context, targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	return filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx, targets, inspectWorktreeGitState)
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
	item  types.DebrisInfo
	path  string
	depth int
	index int
}

func normalizeCleanTargets(targets []types.DebrisInfo) []types.DebrisInfo {
	byPath := make(map[string]normalizedCleanTarget, len(targets))
	for i, target := range targets {
		path, ok := cleanTargetPathKey(target.Path)
		if !ok {
			continue
		}
		candidate := normalizedCleanTarget{
			item:  target,
			path:  path,
			depth: cleanTargetPathDepth(path),
			index: i,
		}
		existing, exists := byPath[path]
		if !exists {
			byPath[path] = candidate
			continue
		}
		if preferCleanTarget(candidate.item, existing.item) {
			existing.item = candidate.item
		}
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

func interactiveClean(targets []types.DebrisInfo) int64 {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting home dir: %v\n", err)
		return 0
	}
	displayHome := resolvedDisplayHome(home)

	var total int64
	scanner := bufio.NewScanner(os.Stdin)
	for _, w := range targets {
		if !cleaner.IsSafeTarget(home, w) {
			fmt.Fprintf(os.Stderr, "  error: unsafe path %q rejected\n", w.Path)
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
			freed, err := cleaner.Execute([]types.DebrisInfo{w})
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				continue
			}
			total += freed
		} else {
			fmt.Printf("  skipped\n")
		}
	}
	return total
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
