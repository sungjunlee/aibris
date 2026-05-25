package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for AI tool worktrees",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		result, err := scanner.Scan(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		if result.TotalCount == 0 {
			fmt.Println("No AI tool worktrees found.")
			return
		}

		currentTool := ""
		for _, w := range result.Worktrees {
			toolName := string(w.Tool)
			if toolName != currentTool {
				currentTool = toolName
				fmt.Printf("\n%s:\n", toolName)
			}
			project := w.Project
			if project == "" {
				project = "?"
			}
			age := time.Since(w.ModTime).Round(time.Hour)
			if age.Hours() < 24 {
				fmt.Printf("  → %-14s %-18s %10s  today\n", w.ID, project, cleaner.FormatSize(w.Size))
			} else {
				fmt.Printf("  → %-14s %-18s %10s  %s ago\n",
					w.ID, project, cleaner.FormatSize(w.Size), ageString(age))
			}
		}
		fmt.Printf("\nTotal: %d worktrees | %s\n", result.TotalCount, cleaner.FormatSize(result.TotalSize))
	},
}

func ageString(d time.Duration) string {
	if d.Hours() < 24 {
		return "today"
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}
