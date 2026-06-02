package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
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

		if scanJSON {
			result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{Roots: scanRoots})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			printJSON(result)
			return
		}

		roots, err := scanner.NormalizeRoots(scanRoots)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printScanHeader(roots)

		result, err := scanner.ScanWithOptions(ctx, types.ScanOptions{
			Roots:      roots,
			OnProgress: printScanProgress,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		printHumanScanResult(result)
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

func printScanHeader(roots []string) {
	fmt.Println("scan")
	fmt.Printf("  roots  %s\n\n", strings.Join(displayRoots(roots), ", "))
}

func printScanProgress(event types.ScanProgressEvent) {
	switch event.State {
	case types.ScanProgressStart:
		fmt.Printf("  running  %-12s\n", event.Tool)
	case types.ScanProgressDone:
		fmt.Printf("  done     %-12s %3d found   %s\n\n",
			event.Tool, event.Count, cleaner.FormatSize(event.Size))
	case types.ScanProgressError:
		fmt.Printf("  error    %-12s %s\n\n", event.Tool, event.Err)
	}
}

func printHumanScanResult(r *types.ScanResult) {
	fmt.Println("summary")
	fmt.Printf("  found       %d %s\n", r.TotalCount, itemNoun(r.TotalCount))
	fmt.Printf("  reclaimable %s\n", cleaner.FormatSize(r.TotalSize))
	if risky := riskyCount(r.Worktrees); risky > 0 {
		fmt.Printf("  risky       %d %s require --risky\n", risky, itemNoun(risky))
	}

	printCategorySummary(r.ByCategory)
	printLargestItems(r.Worktrees)

	fmt.Println("\nnext")
	if r.TotalCount > 0 {
		fmt.Println("  aibris clean --dry-run")
	}
	fmt.Println("  aibris scan --json")
}

func printCategorySummary(summary map[types.Category]types.CategorySummary) {
	if len(summary) == 0 {
		return
	}

	fmt.Println("\nby category")
	for _, category := range sortedCategories(summary) {
		entry := summary[category]
		fmt.Printf("  %-13s %3d   %s\n", category, entry.Count, cleaner.FormatSize(entry.Size))
	}
}

func printLargestItems(items []types.DebrisInfo) {
	if len(items) == 0 {
		return
	}

	limit := 5
	if len(items) < limit {
		limit = len(items)
	}

	fmt.Println("\nlargest")
	for _, item := range items[:limit] {
		fmt.Printf("  %8s  %-13s %-12s %-18s %s\n",
			cleaner.FormatSize(item.Size),
			item.Category,
			itemName(item),
			itemProject(item),
			itemAgeAndStatus(item))
	}
	if len(items) > limit {
		fmt.Printf("  + %d more\n", len(items)-limit)
	}
}

func sortedCategories(summary map[types.Category]types.CategorySummary) []types.Category {
	categories := make([]types.Category, 0, len(summary))
	for category := range summary {
		categories = append(categories, category)
	}
	sort.Slice(categories, func(i, j int) bool {
		left := summary[categories[i]]
		right := summary[categories[j]]
		if left.Size == right.Size {
			return categories[i] < categories[j]
		}
		return left.Size > right.Size
	})
	return categories
}

func displayRoots(roots []string) []string {
	out := make([]string, len(roots))
	home, err := os.UserHomeDir()
	if err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(home); resolveErr == nil {
			home = resolved
		}
	}
	for i, root := range roots {
		displayRoot := root
		if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
			displayRoot = resolved
		}
		if err == nil {
			out[i] = displayHomePath(home, displayRoot)
		} else {
			out[i] = displayRoot
		}
	}
	return out
}

func displayHomePath(home, path string) string {
	rel, err := filepath.Rel(home, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "~"
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return path
	}
	return filepath.Join("~", rel)
}

func riskyCount(items []types.DebrisInfo) int {
	var count int
	for _, item := range items {
		if item.Category.IsRisky() {
			count++
		}
	}
	return count
}

func itemNoun(count int) string {
	if count == 1 {
		return "item"
	}
	return "items"
}

func itemName(item types.DebrisInfo) string {
	if item.ID != "" {
		return item.ID
	}
	return string(item.Tool)
}

func itemProject(item types.DebrisInfo) string {
	if item.Project != "" {
		return item.Project
	}
	return "?"
}

func itemAgeAndStatus(item types.DebrisInfo) string {
	age := ageString(time.Since(item.ModTime).Round(time.Hour))
	if item.Status == "" {
		return age
	}
	return fmt.Sprintf("%s %s", item.Status, age)
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
