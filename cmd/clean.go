package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	cleanAge         string
	cleanCategory    string
	cleanTools       string
	cleanDryRun      bool
	cleanInteractive bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up old AI tool worktrees",
	Run: func(cmd *cobra.Command, args []string) {
		age, err := time.ParseDuration(cleanAge)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid age: %s\n", cleanAge)
			os.Exit(1)
		}

		ctx := context.Background()
		result, err := scanner.Scan(ctx)
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
			Age:         age,
			Categories:  categories,
			Tools:       tools,
			DryRun:      cleanDryRun,
			Interactive: cleanInteractive,
		}

		targets := cleaner.Filter(result.Worktrees, opts)

		if len(targets) == 0 {
			fmt.Println("No worktrees to clean.")
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

		total, err := cleaner.Execute(targets)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
		}
		fmt.Printf("\nFreed: %s\n", cleaner.FormatSize(total))
	},
}

func init() {
	cleanCmd.Flags().StringVarP(&cleanAge, "age", "a", "168h", "Max age in Go duration format (168h = 7 days, 720h = 30 days)")
	cleanCmd.Flags().StringVarP(&cleanCategory, "category", "c", "", "Comma-separated categories (worktree,node_modules)")
	cleanCmd.Flags().StringVarP(&cleanTools, "tool", "t", "", "Comma-separated tools (codex,claude,cursor)")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "Preview without deleting")
	cleanCmd.Flags().BoolVarP(&cleanInteractive, "interactive", "i", false, "Confirm each deletion")
}

func interactiveClean(targets []types.WorktreeInfo) int64 {
	var total int64
	for _, w := range targets {
		fmt.Printf("Remove %s (%s) [%s]? [y/N]: ", w.ID, w.Tool, cleaner.FormatSize(w.Size))
		var response string
		fmt.Scanln(&response)
		if response == "y" || response == "Y" {
			if err := os.RemoveAll(w.Path); err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				continue
			}
			total += w.Size
			fmt.Printf("  removed\n")
		} else {
			fmt.Printf("  skipped\n")
		}
	}
	return total
}
