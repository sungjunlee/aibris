package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

const scanJSONSchemaVersion = scanreport.JSONSchemaVersion

type (
	jsonWorktree               = scanreport.JSONItem
	jsonSummaryEntry           = scanreport.JSONSummaryEntry
	jsonSummary                = scanreport.JSONSummary
	jsonProviderError          = scanreport.JSONProviderError
	jsonProviderDiagnostic     = scanreport.JSONProviderDiagnostic
	jsonRetentionProviderError = scanreport.JSONRetentionProviderError
	jsonRetentionBucket        = scanreport.JSONRetentionBucket
	jsonRetention              = scanreport.JSONRetention
	jsonExcludedScope          = scanreport.JSONExcludedScope
	jsonRejectedExclude        = scanreport.JSONRejectedExclude
	jsonExclusions             = scanreport.JSONExclusions
	jsonVolume                 = scanreport.JSONVolume
	jsonOutput                 = scanreport.JSONOutput
)

var (
	scanJSON        bool
	scanDiagnostics bool
	scanRoots       []string
	scanExcludes    []string
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan for AI tool debris (worktrees, caches, node_modules, logs)",
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		if scanJSON {
			roots, err := scanner.NormalizeRoots(scanRoots)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			result, err := scanner.DefaultScanner.ScanWithOptions(ctx, types.ScanOptions{
				Roots:         roots,
				ExplicitRoots: len(scanRoots) > 0,
				Diagnostics:   scanDiagnostics,
				Excludes:      scanExcludes,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result, len(scanRoots) > 0)
			printJSON(result)
			if result.Partial() {
				os.Exit(1)
			}
			return
		}

		roots, err := scanner.NormalizeRoots(scanRoots)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		printScanHeader(roots)

		progress := newScanProgressPrinter(os.Stdout)
		result, err := scanner.DefaultScanner.ScanWithOptions(ctx, types.ScanOptions{
			Roots:         roots,
			ExplicitRoots: len(scanRoots) > 0,
			Diagnostics:   scanDiagnostics,
			Excludes:      scanExcludes,
			OnProgress:    progress.Handle,
		})
		progress.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result, len(scanRoots) > 0)
		printHumanScanResult(ctx, result)
		if result.Partial() {
			os.Exit(1)
		}
	},
}

func printJSON(r *types.ScanResult) {
	scanreport.WriteJSON(os.Stdout, scanreport.FromResultJSON(r))
}

func printScanHeader(roots []string) {
	fmt.Println("scan")
	fmt.Printf("  roots  %s\n\n", strings.Join(displayRoots(roots), ", "))
}

func isTerminal(file *os.File) bool {
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func printHumanScanResult(ctx context.Context, r *types.ScanResult) {
	view := scanreport.FromResult(r, scanreport.DefaultCleanPolicy())
	view.CodexActivity = codexActivityNotice(ctx, r.Worktrees)
	scanreport.WriteHuman(os.Stdout, view)
}

func codexActivityNotice(ctx context.Context, items []types.DebrisInfo) *scanreport.CodexActivityNotice {
	plan := loadCodexActivityRecommendations(ctx, items)
	if len(plan.Recommendations) == 0 || plan.Activity.Available {
		return nil
	}
	return &scanreport.CodexActivityNotice{ProtectedCount: plan.ProtectedCount}
}

func init() {
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "Output as JSON")
	scanCmd.Flags().BoolVar(&scanDiagnostics, "diagnostics", false, "Report per-provider timing and diagnostics (experimental)")
	scanCmd.Flags().StringArrayVar(&scanRoots, "root", nil, "Scan root under $HOME (repeatable)")
	scanCmd.Flags().StringArrayVar(&scanExcludes, "exclude", nil, "Exclude a path or glob pattern under scan roots from discovery (repeatable)")
}
