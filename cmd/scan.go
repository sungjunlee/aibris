package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	scanJSON  bool
	scanRoots []string
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for AI tool debris (worktrees, caches, node_modules, logs)",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{Roots: scanRoots})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		if scanJSON {
			printJSON(result)
			return
		}

		if result.TotalCount == 0 {
			fmt.Println("No AI tool debris found.")
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
		fmt.Printf("\nTotal: %d items | %s\n", result.TotalCount, cleaner.FormatSize(result.TotalSize))
	},
}

type jsonWorktree struct {
	Tool           string   `json:"tool"`
	Category       string   `json:"category"`
	ID             string   `json:"id"`
	Project        string   `json:"project"`
	Path           string   `json:"path"`
	Size           int64    `json:"size"`
	ModTime        string   `json:"mod_time"`
	Status         string   `json:"status"`
	Risk           string   `json:"risk"`
	Reason         string   `json:"reason"`
	CleanupKind    string   `json:"cleanup_kind"`
	CleanupCommand []string `json:"cleanup_command"`
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
		cleanupCommand := append([]string(nil), w.CleanupCommand...)
		if cleanupCommand == nil {
			cleanupCommand = []string{}
		}
		out.Worktrees[i] = jsonWorktree{
			Tool:           string(w.Tool),
			Category:       string(w.Category),
			ID:             w.ID,
			Project:        w.Project,
			Path:           w.Path,
			Size:           w.Size,
			ModTime:        w.ModTime.Format(time.RFC3339),
			Status:         string(w.Status),
			Risk:           itemRisk(w),
			Reason:         itemReason(w),
			CleanupKind:    string(cleanupKind(w)),
			CleanupCommand: cleanupCommand,
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

func cleanupKind(w types.DebrisInfo) types.CleanupKind {
	if w.CleanupKind != "" {
		return w.CleanupKind
	}
	return types.CleanupRemovePath
}

func itemRisk(w types.DebrisInfo) string {
	if w.Category.IsRisky() {
		return "high"
	}
	switch w.Category {
	case types.CategoryNodeModules, types.CategoryBuildCache:
		return "medium"
	default:
		return "low"
	}
}

func itemReason(w types.DebrisInfo) string {
	switch w.Category {
	case types.CategoryWorktree:
		switch w.Status {
		case types.WorktreeActive:
			return "active worktree; protected from cleanup by default"
		case types.WorktreeOrphaned:
			return "orphaned worktree; parent repo metadata missing"
		default:
			return "worktree debris"
		}
	case types.CategoryNodeModules:
		return "dependency directory; can be reinstalled"
	case types.CategoryBuildCache:
		return "build cache; can be regenerated"
	case types.CategoryOtherCache:
		return "package cache; can be regenerated"
	case types.CategoryAILogs:
		return "AI tool logs; requires --risky to clean"
	default:
		return "unknown category; requires explicit review"
	}
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
	scanCmd.Flags().StringArrayVar(&scanRoots, "root", nil, "Scan root under $HOME (repeatable)")
}
