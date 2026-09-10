package cmd

import (
	"strings"

	"github.com/spf13/cobra"
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
	cleanProtectPaths           []string
	cleanIncludeActiveWorktrees bool
	cleanAgentStateGrace        string
	cleanReceiptFile            string
	cleanAPFSSnapshots          bool
	cleanPressure               bool
)

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

--protect-path is clean-only: a nested checkout under a worktree outer owner
protects that owner from delete and strip. Protected items stay in the plan
with an explicit reason. Scan inventory and --exclude discovery-hide matching
are unchanged. Sibling cache paths are not auto-protected.

--strip is a separate disposition from deletion: it removes only the
regenerable subtrees (dependency directories and platform build output)
inventoried inside worktree units that deletion protects, recovering space
without deleting the unit, its branch, or any uncommitted work.`,
	Run: func(cmd *cobra.Command, args []string) {
		runCleanCommand(cmd)
	},
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
	cleanCmd.Flags().StringArrayVar(&cleanProtectPaths, "protect-path", nil, "Protect a path under scan roots from delete and strip (repeatable). A nested checkout protects its worktree outer owner; protected items stay in the plan")
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
