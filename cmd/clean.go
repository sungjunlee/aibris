package cmd

import (
	"bufio"
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
	cleanInteractive            bool
	cleanRisky                  bool
	cleanForce                  bool
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

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{Roots: cleanRoots})
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

		targets := cleaner.Filter(result.Worktrees, opts)

		if len(targets) == 0 {
			fmt.Println("No items to clean.")
			return
		}

		if opts.DryRun {
			cleaner.DryRun(targets)
			return
		}

		if opts.Interactive {
			total := interactiveClean(targets)
			fmt.Printf("\nFreed: %s\n", cleaner.FormatSize(total))
			return
		}

		if !opts.Force {
			var totalSize int64
			for _, w := range targets {
				totalSize += w.Size
			}
			fmt.Printf("About to delete %d items (%s).\n", len(targets), cleaner.FormatSize(totalSize))
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
		fmt.Printf("\nFreed: %s\n", cleaner.FormatSize(total))
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
	cleanCmd.Flags().StringArrayVar(&cleanRoots, "root", nil, "Scan root under $HOME (repeatable)")
	cleanCmd.Flags().BoolVar(&cleanIncludeActiveWorktrees, "include-active-worktrees", false, "Include active worktrees in cleanup candidates")
}

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

func interactiveClean(targets []types.DebrisInfo) int64 {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting home dir: %v\n", err)
		return 0
	}

	var total int64
	scanner := bufio.NewScanner(os.Stdin)
	for _, w := range targets {
		if !cleaner.IsSafePath(home, w.Path) {
			fmt.Fprintf(os.Stderr, "  error: unsafe path %q rejected\n", w.Path)
			continue
		}
		fmt.Printf("Remove %s (%s) [%s]? [y/N]: ", w.ID, w.Tool, cleaner.FormatSize(w.Size))
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
