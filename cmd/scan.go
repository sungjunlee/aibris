package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

var scanJSON bool

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

		if scanJSON {
			printJSON(result)
			return
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

type jsonWorktree struct {
	Tool     string `json:"tool"`
	Category string `json:"category"`
	ID       string `json:"id"`
	Project  string `json:"project"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mod_time"`
}

type jsonSummaryEntry struct {
	Count int   `json:"count"`
	Size  int64 `json:"size"`
}

type jsonSummary struct {
	TotalCount int                         `json:"total_count"`
	TotalSize  int64                       `json:"total_size"`
	ByCategory map[string]jsonSummaryEntry `json:"by_category"`
	ByTool     map[string]jsonSummaryEntry `json:"by_tool"`
}

type jsonOutput struct {
	Worktrees []jsonWorktree `json:"worktrees"`
	Summary   jsonSummary    `json:"summary"`
}

func printJSON(r *types.ScanResult) {
	out := jsonOutput{
		Worktrees: make([]jsonWorktree, len(r.Worktrees)),
		Summary: jsonSummary{
			TotalCount: r.TotalCount,
			TotalSize:  r.TotalSize,
			ByCategory: make(map[string]jsonSummaryEntry, len(r.ByCategory)),
			ByTool:     make(map[string]jsonSummaryEntry, len(r.ByTool)),
		},
	}
	for i, w := range r.Worktrees {
		out.Worktrees[i] = jsonWorktree{
			Tool:     string(w.Tool),
			Category: string(w.Category),
			ID:       w.ID,
			Project:  w.Project,
			Path:     w.Path,
			Size:     w.Size,
			ModTime:  w.ModTime.Format(time.RFC3339),
		}
	}
	for cat, s := range r.ByCategory {
		out.Summary.ByCategory[string(cat)] = jsonSummaryEntry{Count: s.Count, Size: s.Size}
	}
	for tool, s := range r.ByTool {
		out.Summary.ByTool[string(tool)] = jsonSummaryEntry{Count: s.Count, Size: s.Size}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

func ageString(d time.Duration) string {
	if d.Hours() < 24 {
		return "today"
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}

func init() {
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "Output as JSON")
}
