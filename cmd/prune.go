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
	pruneAge         string
	pruneTools       string
	pruneAll         bool
	pruneDryRun      bool
	pruneForce       bool
	pruneInteractive bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove old AI tool worktrees",
	Run: func(cmd *cobra.Command, args []string) {
		age, err := time.ParseDuration(pruneAge)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid age: %s\n", pruneAge)
			os.Exit(1)
		}

		ctx := context.Background()
		result, err := scanner.Scan(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		var tools []types.Tool
		if pruneTools != "" && !pruneAll {
			for _, t := range strings.Split(pruneTools, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tools = append(tools, types.Tool(t))
				}
			}
		}

		opts := types.PruneOptions{
			Age:         age,
			Tools:       tools,
			All:         pruneAll || len(tools) == 0,
			DryRun:      pruneDryRun,
			Interactive: pruneInteractive,
			Force:       pruneForce,
		}

		targets := cleaner.Filter(result.Worktrees, opts)

		if len(targets) == 0 {
			fmt.Println("No worktrees to prune.")
			return
		}

		if opts.DryRun {
			cleaner.DryRun(targets)
			return
		}

		var total int64
		if opts.Interactive {
			total = interactivePrune(targets)
		} else {
			if !opts.Force {
				fmt.Printf("WARNING: This will remove %d worktrees (%s)\n",
					len(targets), cleaner.FormatSize(sumSizes(targets)))
				fmt.Print("Proceed? [y/N]: ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					fmt.Println("Aborted.")
					return
				}
			}
			total, err = cleaner.Execute(targets)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
			}
		}
		fmt.Printf("\nFreed: %s\n", cleaner.FormatSize(total))
	},
}

func init() {
	pruneCmd.Flags().StringVarP(&pruneAge, "age", "a", "168h", "Max age (e.g. 24h, 7d, 30d)")
	pruneCmd.Flags().StringVarP(&pruneTools, "tool", "t", "", "Comma-separated tools (codex,claude,cursor)")
	pruneCmd.Flags().BoolVar(&pruneAll, "all", false, "Target all tools")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Preview without deleting")
	pruneCmd.Flags().BoolVarP(&pruneForce, "force", "f", false, "Skip confirmation")
	pruneCmd.Flags().BoolVarP(&pruneInteractive, "interactive", "i", false, "Confirm each deletion")
}

func interactivePrune(targets []types.WorktreeInfo) int64 {
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

func sumSizes(worktrees []types.WorktreeInfo) int64 {
	var total int64
	for _, w := range worktrees {
		total += w.Size
	}
	return total
}
