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
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

var (
	scanJSON        bool
	scanDiagnostics bool
	scanRoots       []string
	scanExcludes    []string
)

// scanJSONSchemaVersion is the version of the top-level `scan --json`
// contract. Consumers should treat an unknown version as unsupported. The
// historical `worktrees` field stays as a 0.x compatibility alias for the
// canonical `items` array during the 0.x period (see docs/JSON_SCHEMA.md).
const scanJSONSchemaVersion = 1

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
				Roots:       roots,
				Diagnostics: scanDiagnostics,
				Excludes:    scanExcludes,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result)
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
			Roots:       roots,
			Diagnostics: scanDiagnostics,
			Excludes:    scanExcludes,
			OnProgress:  progress.Handle,
		})
		progress.Stop()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}

		writeLastScanCache(roots, scanner.DefaultScanner.ProviderIdentity(), result)
		printHumanScanResult(ctx, result)
		if result.Partial() {
			os.Exit(1)
		}
	},
}

type jsonWorktree struct {
	Tool            string   `json:"tool"`
	Category        string   `json:"category"`
	ID              string   `json:"id"`
	Project         string   `json:"project"`
	Source          string   `json:"source"`
	Path            string   `json:"path"`
	Size            int64    `json:"size"`
	ModTime         string   `json:"mod_time"`
	Status          string   `json:"status"`
	Classification  string   `json:"classification,omitempty"`
	Risk            string   `json:"risk"`
	Reason          string   `json:"reason"`
	CleanupKind     string   `json:"cleanup_kind"`
	CleanupCommand  []string `json:"cleanup_command"`
	StrippableBytes int64    `json:"strippable_bytes,omitempty"`
	StrippablePaths []string `json:"strippable_paths,omitempty"`
}

type jsonSummaryEntry struct {
	Count           int   `json:"count"`
	Size            int64 `json:"size"`
	StrippableBytes int64 `json:"strippable_bytes,omitempty"`
}

type jsonSummary struct {
	TotalCount           int                         `json:"total_count"`
	TotalSize            int64                       `json:"total_size"`
	TotalStrippableBytes int64                       `json:"total_strippable_bytes,omitempty"`
	ByCategory           map[string]jsonSummaryEntry `json:"by_category"`
	ByTool               map[string]jsonSummaryEntry `json:"by_tool"`
}

type jsonProviderError struct {
	Tool    string `json:"tool"`
	Message string `json:"message"`
}

// jsonProviderDiagnostic is experimental; see docs/JSON_SCHEMA.md.
type jsonProviderDiagnostic struct {
	Tool       string `json:"tool"`
	State      string `json:"state"`
	Count      int    `json:"count"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type jsonRetentionProviderError struct {
	StoreID string `json:"store_id"`
	Message string `json:"message"`
}

type jsonRetentionBucket struct {
	StoreID       string `json:"store_id"`
	BucketID      string `json:"bucket_id"`
	UnitCount     int    `json:"unit_count"`
	MemberCount   int    `json:"member_count"`
	ApparentBytes int64  `json:"apparent_bytes"`
	OrphanedCount int    `json:"orphaned_count"`
	OrphanedBytes int64  `json:"orphaned_bytes"`
}

type jsonRetention struct {
	Buckets        []jsonRetentionBucket        `json:"buckets"`
	Partial        bool                         `json:"partial"`
	ProviderErrors []jsonRetentionProviderError `json:"provider_errors"`
}

type jsonExcludedScope struct {
	Pattern  string `json:"pattern"`
	Resolved string `json:"resolved"`
	Source   string `json:"source"`
	Count    int    `json:"count"`
}

type jsonRejectedExclude struct {
	Pattern string `json:"pattern"`
	Source  string `json:"source"`
	Reason  string `json:"reason"`
}

type jsonExclusions struct {
	ExcludedCount int                   `json:"excluded_count"`
	Scopes        []jsonExcludedScope   `json:"scopes"`
	Rejected      []jsonRejectedExclude `json:"rejected"`
}

type jsonVolume struct {
	Role                   string  `json:"role"`
	FSType                 string  `json:"fs_type"`
	ID                     string  `json:"id"`
	TotalBytes             uint64  `json:"total_bytes"`
	UsedBytes              uint64  `json:"used_bytes"`
	AvailableBytes         uint64  `json:"available_bytes"`
	UsedPercent            float64 `json:"used_percent"`
	Band                   string  `json:"band"`
	DebrisBytes            int64   `json:"debris_bytes"`
	OtherVolumeDebrisBytes int64   `json:"other_volume_debris_bytes,omitempty"`
}

type jsonOutput struct {
	SchemaVersion  int                      `json:"schema_version"`
	Items          []jsonWorktree           `json:"items"`
	Worktrees      []jsonWorktree           `json:"worktrees"`
	Summary        jsonSummary              `json:"summary"`
	Retention      jsonRetention            `json:"retention"`
	Volume         *jsonVolume              `json:"volume,omitempty"`
	Exclusions     *jsonExclusions          `json:"exclusions,omitempty"`
	Partial        bool                     `json:"partial,omitempty"`
	ProviderErrors []jsonProviderError      `json:"provider_errors,omitempty"`
	Diagnostics    []jsonProviderDiagnostic `json:"diagnostics,omitempty"`
}

func printJSON(r *types.ScanResult) {
	items := make([]jsonWorktree, len(r.Worktrees))
	out := jsonOutput{
		SchemaVersion: scanJSONSchemaVersion,
		Worktrees:     items,
		Items:         items,
		Partial:       r.Partial(),
		Retention: jsonRetention{
			Buckets:        make([]jsonRetentionBucket, len(r.Retention.Buckets)),
			Partial:        r.Retention.Partial,
			ProviderErrors: make([]jsonRetentionProviderError, len(r.Retention.ProviderErrors)),
		},
		Summary: jsonSummary{
			TotalCount:           r.TotalCount,
			TotalSize:            r.TotalSize,
			TotalStrippableBytes: r.TotalStrippableBytes,
			ByCategory:           make(map[string]jsonSummaryEntry, len(r.ByCategory)),
			ByTool:               make(map[string]jsonSummaryEntry, len(r.ByTool)),
		},
	}
	for _, providerErr := range r.ProviderErrors {
		out.ProviderErrors = append(out.ProviderErrors, jsonProviderError{
			Tool:    string(providerErr.Tool),
			Message: providerErr.Message,
		})
	}
	for _, diagnostic := range r.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, jsonProviderDiagnostic{
			Tool:       string(diagnostic.Tool),
			State:      string(diagnostic.State),
			Count:      diagnostic.Count,
			Bytes:      diagnostic.Bytes,
			DurationMS: diagnostic.Duration.Milliseconds(),
			Error:      diagnostic.Err,
		})
	}
	if r.ExcludedByUser > 0 || len(r.ExcludedScopes) > 0 || len(r.RejectedExcludes) > 0 {
		out.Exclusions = &jsonExclusions{
			ExcludedCount: r.ExcludedByUser,
			Scopes:        make([]jsonExcludedScope, 0, len(r.ExcludedScopes)),
			Rejected:      make([]jsonRejectedExclude, 0, len(r.RejectedExcludes)),
		}
		for _, scope := range r.ExcludedScopes {
			out.Exclusions.Scopes = append(out.Exclusions.Scopes, jsonExcludedScope{
				Pattern:  scope.Pattern,
				Resolved: scope.Resolved,
				Source:   string(scope.Source),
				Count:    scope.Count,
			})
		}
		for _, rejected := range r.RejectedExcludes {
			out.Exclusions.Rejected = append(out.Exclusions.Rejected, jsonRejectedExclude{
				Pattern: rejected.Pattern,
				Source:  string(rejected.Source),
				Reason:  rejected.Reason,
			})
		}
	}
	for i, w := range r.Worktrees {
		cleanupCommand := append([]string(nil), w.CleanupCommand...)
		if cleanupCommand == nil {
			cleanupCommand = []string{}
		}
		items[i] = jsonWorktree{
			Tool:            string(w.Tool),
			Category:        string(w.Category),
			ID:              w.ID,
			Project:         w.Project,
			Source:          w.Source,
			Path:            w.Path,
			Size:            w.Size,
			ModTime:         w.ModTime.Format(time.RFC3339),
			Status:          string(w.Status),
			Classification:  string(w.Classification),
			Risk:            itemRisk(w),
			Reason:          itemReason(w),
			CleanupKind:     string(cleanupKind(w)),
			CleanupCommand:  cleanupCommand,
			StrippableBytes: w.StrippableBytes,
			StrippablePaths: append([]string(nil), w.StrippablePaths...),
		}
	}
	for i, bucket := range r.Retention.Buckets {
		out.Retention.Buckets[i] = jsonRetentionBucket{
			StoreID:       string(bucket.StoreID),
			BucketID:      bucket.BucketID,
			UnitCount:     bucket.UnitCount,
			MemberCount:   bucket.MemberCount,
			ApparentBytes: bucket.ApparentBytes,
			OrphanedCount: bucket.OrphanedCount,
			OrphanedBytes: bucket.OrphanedBytes,
		}
	}
	for i, providerErr := range r.Retention.ProviderErrors {
		out.Retention.ProviderErrors[i] = jsonRetentionProviderError{
			StoreID: string(providerErr.StoreID),
			Message: providerErr.Message,
		}
	}
	if report := homeVolumeReport(r.Worktrees); report != nil {
		out.Volume = jsonVolumeFromReport(*report)
	}
	for cat, s := range r.ByCategory {
		out.Summary.ByCategory[string(cat)] = jsonSummaryEntry{Count: s.Count, Size: s.Size, StrippableBytes: s.StrippableBytes}
	}
	for tool, s := range r.ByTool {
		out.Summary.ByTool[string(tool)] = jsonSummaryEntry{Count: s.Count, Size: s.Size, StrippableBytes: s.StrippableBytes}
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
	label := scanProgressLabel(event.Tool)
	switch event.State {
	case types.ScanProgressStart:
		fmt.Printf("  scanning %-12s\n", label)
	case types.ScanProgressDone:
		fmt.Printf("  found    %-12s %3d items   %s\n\n",
			label, event.Count, cleaner.FormatSize(event.Size))
	case types.ScanProgressError:
		fmt.Printf("  error    %-12s %s\n\n", label, event.Err)
	}
}

// scanProgressLabel is the human name for a provider in scan progress.
// The worktree adapter keeps Name() == "codex" for cache identity and
// --tool selectors; progress must not label every worktree row as Codex.
func scanProgressLabel(tool types.Tool) string {
	if tool == types.ToolCodex {
		return "worktree"
	}
	return string(tool)
}

type scanProgressPrinter struct {
	out         *os.File
	interactive bool
	mu          sync.Mutex
	stop        chan struct{}
	stopped     chan struct{}
	stopOnce    sync.Once
	active      map[types.Tool]bool
	started     int
	done        int
	items       int
	size        int64
	errors      int
	frame       int
}

func newScanProgressPrinter(out *os.File) *scanProgressPrinter {
	p := &scanProgressPrinter{
		out:         out,
		interactive: isTerminal(out),
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
		active:      make(map[types.Tool]bool),
	}
	if p.interactive {
		go p.spin()
	}
	return p
}

func (p *scanProgressPrinter) Handle(event types.ScanProgressEvent) {
	if !p.interactive {
		printScanProgress(event)
		return
	}

	p.mu.Lock()
	switch event.State {
	case types.ScanProgressStart:
		if !p.active[event.Tool] {
			p.started++
		}
		p.active[event.Tool] = true
	case types.ScanProgressDone:
		delete(p.active, event.Tool)
		p.done++
		p.items += event.Count
		p.size += event.Size
	case types.ScanProgressError:
		delete(p.active, event.Tool)
		p.done++
		p.errors++
	}
	p.renderLocked()
	p.mu.Unlock()
}

func (p *scanProgressPrinter) Stop() {
	if !p.interactive {
		return
	}

	p.stopOnce.Do(func() {
		close(p.stop)
		<-p.stopped
		p.mu.Lock()
		defer p.mu.Unlock()
		fmt.Fprint(p.out, "\r\x1b[2K")
		if p.started == 0 {
			return
		}
		if p.errors > 0 {
			fmt.Fprintf(p.out, "  scanned  %d sources   %d items   %s   %d errors\n\n",
				p.started, p.items, cleaner.FormatSize(p.size), p.errors)
			return
		}
		fmt.Fprintf(p.out, "  scanned  %d sources   %d items   %s\n\n",
			p.started, p.items, cleaner.FormatSize(p.size))
	})
}

func (p *scanProgressPrinter) spin() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	defer close(p.stopped)

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			p.frame++
			p.renderLocked()
			p.mu.Unlock()
		case <-p.stop:
			return
		}
	}
}

func (p *scanProgressPrinter) renderLocked() {
	if p.started == 0 {
		return
	}
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	active := activeTools(p.active)
	if active == "" {
		if p.done >= p.started {
			return
		}
		active = "wrapping up"
	}
	fmt.Fprintf(p.out, "\r\x1b[2K  %s scanning %s   %d/%d done   %d items   %s",
		frames[p.frame%len(frames)], active, p.done, p.started, p.items, cleaner.FormatSize(p.size))
}

func activeTools(active map[types.Tool]bool) string {
	if len(active) == 0 {
		return ""
	}
	tools := make([]string, 0, len(active))
	for tool := range active {
		tools = append(tools, scanProgressLabel(tool))
	}
	sort.Strings(tools)
	if len(tools) > 3 {
		return strings.Join(tools[:3], ", ") + "..."
	}
	return strings.Join(tools, ", ")
}

func isTerminal(file *os.File) bool {
	stat, err := file.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func printHumanScanResult(ctx context.Context, r *types.ScanResult) {
	fmt.Println("summary")
	if r.Partial() {
		fmt.Println("  completeness partial (results are incomplete)")
		for _, providerErr := range r.ProviderErrors {
			fmt.Printf("  failed      %-12s %s\n", providerErr.Tool, providerErr.Message)
		}
	}
	report := homeVolumeReport(r.Worktrees)
	printScanHeadline(r, report)
	fmt.Printf("  found       %d %s\n", r.TotalCount, itemNoun(r.TotalCount))
	fmt.Printf("  found size  %s\n", cleaner.FormatSize(r.TotalSize))
	printVolumePressureReport(report)
	if r.TotalStrippableBytes > 0 {
		fmt.Printf("  strippable  %s regenerable subtrees inside worktrees (clean --strip)\n",
			cleaner.FormatSize(r.TotalStrippableBytes))
	}
	if r.Partial() {
		fmt.Println("  default clean unavailable until a complete scan succeeds")
	} else {
		// Mirror clean's own defaults, including the agent-state idle floor:
		// the AI workflow starts from this estimate, so a figure that counts
		// entries clean would not select is worse than no figure.
		defaultPolicy := scanDefaultCleanPolicy()
		diagnostics := summarizeCleanup(r.Worktrees, defaultPolicy)
		fmt.Printf("  default clean (estimate) %s\n", cleaner.FormatSize(diagnostics.EligibleSize))
		printCleanupDiagnostics(diagnostics, defaultPolicy)
	}

	printExclusionDiagnostics(r)
	printCategorySummary(r.ByCategory)
	printLargestItems(r.Worktrees)
	printRetentionProjection(r.Retention)
	printCodexActivityRecommendations(ctx, r.Worktrees)
	printProviderDiagnostics(r)

	printScanNext(r)
}

func scanDefaultCleanPolicy() types.PruneOptions {
	opts := types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	opts.RelaxCacheAge, opts.PressureDevice = shouldRelaxCacheAge(false)
	return opts
}

type reclaimPath struct {
	label   string
	size    int64
	command string
}

func printScanNext(r *types.ScanResult) {
	fmt.Println("\nnext")
	if r.Partial() {
		fmt.Println("  retry aibris scan; cleanup is disabled for this result")
	} else {
		printReclaimLadder(scanReclaimPaths(r.Worktrees, scanDefaultCleanPolicy()))
	}
	printReviewOnlyWorktrees(r.Worktrees)
	fmt.Println("  aibris scan --json")
}

func printScanHeadline(r *types.ScanResult, report *volume.Report) {
	if r.Partial() {
		return
	}
	printScanHeadlinePaths(
		r.TotalSize,
		scanReclaimPaths(r.Worktrees, scanDefaultCleanPolicy()),
		report,
	)
}

func printScanHeadlinePaths(found int64, paths []reclaimPath, report *volume.Report) {
	fmt.Println(scanHeadline(found, paths, report))
	printPressureHint(paths, report)
}

func scanHeadline(found int64, paths []reclaimPath, report *volume.Report) string {
	parts := []string{fmt.Sprintf("%s found", cleaner.FormatSize(found))}
	if path, ok := largestNonDefaultReclaim(paths); ok {
		parts = append(parts, fmt.Sprintf("largest reclaim %s (%s)",
			cleaner.FormatSize(path.size), reclaimFlag(path)))
	}
	if report != nil {
		parts = append(parts, fmt.Sprintf("%.0f%% used   %s free   %s",
			report.UsedPercent,
			cleaner.FormatSize(int64(report.AvailableBytes)),
			volume.HumanWord(report.Band)))
	}
	return "  " + strings.Join(parts, "   ")
}

func largestNonDefaultReclaim(paths []reclaimPath) (reclaimPath, bool) {
	def := reclaimSizeByLabel(paths, "default delete")
	var best reclaimPath
	found := false
	for _, path := range paths {
		if path.label == "default delete" || path.size <= def {
			continue
		}
		if !found || path.size > best.size {
			best = path
			found = true
		}
	}
	return best, found
}

func reclaimSizeByLabel(paths []reclaimPath, label string) int64 {
	for _, path := range paths {
		if path.label == label {
			return path.size
		}
	}
	return 0
}

func reclaimFlag(path reclaimPath) string {
	switch path.label {
	case "pressure caches":
		return "--pressure"
	case "strip (keep trees)":
		return "--strip"
	default:
		return "default"
	}
}

func printPressureHint(paths []reclaimPath, report *volume.Report) {
	if report == nil || (report.Band != volume.BandLow && report.Band != volume.BandCritical) {
		return
	}
	pressure := reclaimSizeByLabel(paths, "pressure caches")
	if pressure <= 0 {
		return
	}
	if largest, ok := largestNonDefaultReclaim(paths); ok && reclaimFlag(largest) == "--pressure" {
		return
	}
	fmt.Printf("  reclaim --pressure %s\n", cleaner.FormatSize(pressure))
}

func isReviewOnlyWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Status != types.WorktreeActive &&
		item.Status != types.WorktreeOrphaned
}

func countReviewOnlyWorktrees(items []types.DebrisInfo) int {
	n := 0
	for _, item := range items {
		if isReviewOnlyWorktree(item) {
			n++
		}
	}
	return n
}

func printReviewOnlyWorktrees(items []types.DebrisInfo) {
	printReviewOnlyWorktreeCount(countReviewOnlyWorktrees(items))
}

func printReviewOnlyWorktreeCount(n int) {
	if n == 0 {
		return
	}
	noun := "units"
	if n == 1 {
		noun = "unit"
	}
	fmt.Printf("  review-only worktrees  %d %s   not cleanable; inspect mixed/missing .git markers\n",
		n, noun)
}

func printReclaimLadder(paths []reclaimPath) {
	for _, path := range paths {
		fmt.Printf("  %-20s %10s   %s\n", path.label, cleaner.FormatSize(path.size), path.command)
	}
}

func scanReclaimPaths(items []types.DebrisInfo, defaultPolicy types.PruneOptions) []reclaimPath {
	var paths []reclaimPath
	defaultSize := summarizeCleanup(items, defaultPolicy).EligibleSize
	paths = appendReclaimPath(paths, "default delete", defaultSize, "aibris clean --dry-run")
	stripSize := scanStripEstimate(items, defaultPolicy)
	paths = appendReclaimPath(paths, "strip (keep trees)", stripSize, "aibris clean --strip --dry-run")
	if pressure := scanPressureEstimate(items, defaultPolicy); pressure > defaultSize {
		paths = appendReclaimPath(paths, "pressure caches", pressure, "aibris clean --pressure --dry-run")
	}
	return paths
}

func appendReclaimPath(paths []reclaimPath, label string, size int64, command string) []reclaimPath {
	if size <= 0 {
		return paths
	}
	return append(paths, reclaimPath{label: label, size: size, command: command})
}

func scanStripEstimate(items []types.DebrisInfo, opts types.PruneOptions) int64 {
	cwd, _ := os.Getwd()
	return stripEstimateForCWD(items, opts, cwd)
}

func stripEstimateForCWD(items []types.DebrisInfo, opts types.PruneOptions, cwd string) int64 {
	targets, _ := selectStripTargets(items, opts, cwd)
	var total int64
	for _, target := range targets {
		total += target.StrippableBytes
	}
	return total
}

func scanPressureEstimate(items []types.DebrisInfo, defaultPolicy types.PruneOptions) int64 {
	pressure := defaultPolicy
	pressure.RelaxCacheAge = true
	pressure.PressureDevice = ""
	return summarizeCleanup(items, pressure).EligibleSize
}

func homeVolumeReport(items []types.DebrisInfo) *volume.Report {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	report, err := volume.Inspect(home)
	if err != nil {
		return nil
	}
	report.Role = "home"
	dev, err := volume.PathDevice(home)
	if err != nil {
		dev = ""
	}
	on, other := volume.SplitDebris(dev, items)
	report.DebrisBytes = on
	report.OtherVolumeDebrisBytes = other
	return &report
}

func jsonVolumeFromReport(report volume.Report) *jsonVolume {
	return &jsonVolume{
		Role:                   report.Role,
		FSType:                 report.FSType,
		ID:                     report.ID,
		TotalBytes:             report.TotalBytes,
		UsedBytes:              report.UsedBytes,
		AvailableBytes:         report.AvailableBytes,
		UsedPercent:            report.UsedPercent,
		Band:                   string(report.Band),
		DebrisBytes:            report.DebrisBytes,
		OtherVolumeDebrisBytes: report.OtherVolumeDebrisBytes,
	}
}

func printVolumePressure(items []types.DebrisInfo) {
	printVolumePressureReport(homeVolumeReport(items))
}

func printVolumePressureReport(report *volume.Report) {
	if report == nil {
		return
	}
	printHomeVolumeLine(report)
	if report.OtherVolumeDebrisBytes > 0 {
		fmt.Printf("  debris     %s on this volume   %s other volumes\n",
			cleaner.FormatSize(report.DebrisBytes),
			cleaner.FormatSize(report.OtherVolumeDebrisBytes))
		return
	}
	fmt.Printf("  debris     %s on this volume\n", cleaner.FormatSize(report.DebrisBytes))
}

func printHomeVolumeLine(report *volume.Report) {
	fmt.Printf("  volume     %s  %s  %.0f%% used   %s free   %s\n",
		report.Role, report.FSType, report.UsedPercent,
		cleaner.FormatSize(int64(report.AvailableBytes)), volume.HumanWord(report.Band))
}

func printExclusionDiagnostics(r *types.ScanResult) {
	if r.ExcludedByUser == 0 && len(r.ExcludedScopes) == 0 && len(r.RejectedExcludes) == 0 {
		return
	}

	flagPatterns, filePatterns := excludeSourceCounts(r)
	fmt.Println("\nexclusions (discovery only)")
	fmt.Printf("  patterns  %d flag, %d ignore-file\n", flagPatterns, filePatterns)
	if r.ExcludedByUser > 0 {
		fmt.Printf("  excluded  %d %s hidden from discovery\n", r.ExcludedByUser, itemNoun(r.ExcludedByUser))
	}

	home := ""
	if userHome, err := os.UserHomeDir(); err == nil {
		home = resolvedDisplayHome(userHome)
	}
	for _, scope := range r.ExcludedScopes {
		display := scope.Resolved
		if home != "" {
			display = displayHomePath(home, scope.Resolved)
		}
		fmt.Printf("  scope     %-11s %s  %d %s\n", scope.Source, display, scope.Count, itemNoun(scope.Count))
	}
	for _, rejected := range r.RejectedExcludes {
		fmt.Printf("  rejected  %-11s %s  %s\n", rejected.Source, rejected.Pattern, rejected.Reason)
	}
}

func excludeSourceCounts(r *types.ScanResult) (flagPatterns, filePatterns int) {
	for _, scope := range r.ExcludedScopes {
		if scope.Source == types.ExcludeSourceFlag {
			flagPatterns++
		} else {
			filePatterns++
		}
	}
	for _, rejected := range r.RejectedExcludes {
		if rejected.Source == types.ExcludeSourceFlag {
			flagPatterns++
		} else {
			filePatterns++
		}
	}
	return flagPatterns, filePatterns
}

func printRetentionProjection(projection types.RetentionProjection) {
	if len(projection.Buckets) == 0 && !projection.Partial {
		return
	}

	fmt.Println("\nretention (protected content, read-only)")
	if projection.Partial {
		fmt.Println("  completeness partial (retention inventory only)")
		for _, providerErr := range projection.ProviderErrors {
			fmt.Printf("  failed      %-16s %s\n", providerErr.StoreID, providerErr.Message)
		}
	}
	for _, bucket := range projection.Buckets {
		fmt.Printf("  %-16s %7s  units %d  members %d  %s  orphaned %d/%s\n",
			bucket.StoreID,
			bucket.BucketID,
			bucket.UnitCount,
			bucket.MemberCount,
			cleaner.FormatSize(bucket.ApparentBytes),
			bucket.OrphanedCount,
			cleaner.FormatSize(bucket.OrphanedBytes),
		)
	}
}

func printProviderDiagnostics(r *types.ScanResult) {
	if len(r.Diagnostics) == 0 {
		return
	}

	fmt.Println("\ndiagnostics (experimental)")
	for _, diagnostic := range r.Diagnostics {
		line := fmt.Sprintf("  %-12s %-5s %3d %s   %s   %s",
			diagnostic.Tool,
			diagnostic.State,
			diagnostic.Count,
			itemNoun(diagnostic.Count),
			cleaner.FormatSize(diagnostic.Bytes),
			diagnostic.Duration,
		)
		if diagnostic.Err != "" {
			line += "  " + diagnostic.Err
		}
		fmt.Println(line)
	}
}

func printCodexActivityRecommendations(ctx context.Context, items []types.DebrisInfo) {
	plan := loadCodexActivityRecommendations(ctx, items)
	if len(plan.Recommendations) == 0 || plan.Activity.Available {
		return
	}

	fmt.Println("\ncodex activity")
	fmt.Printf("  unavailable; %d active Codex %s protected by default\n",
		plan.ProtectedCount, codexWorktreeNoun(plan.ProtectedCount))
}

func codexWorktreeNoun(count int) string {
	if count == 1 {
		return "worktree"
	}
	return "worktrees"
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

func itemNoun(count int) string {
	if count == 1 {
		return "item"
	}
	return "items"
}

type cleanupDiagnostics struct {
	EligibleCount               int
	EligibleSize                int64
	ActiveCount                 int
	ActiveSize                  int64
	RiskyCount                  int
	RiskySize                   int64
	AgeCount                    int
	AgeSize                     int64
	FilterCount                 int
	FilterSize                  int64
	AgentStateLiveCount         int
	AgentStateLiveSize          int64
	AgentStateUndeterminedCount int
	AgentStateUndeterminedSize  int64
	OtherBlocked                map[cleaner.EligibilityReason]cleanupDiagnosticBucket
}

type cleanupDiagnosticBucket struct {
	Count int
	Size  int64
}

func summarizeCleanup(items []types.DebrisInfo, opts types.PruneOptions) cleanupDiagnostics {
	observedAt := time.Now()
	var summary cleanupDiagnostics
	var eligible []types.DebrisInfo
	for _, item := range items {
		isEligible, reason := cleaner.EvaluateEligibility(item, opts, observedAt)
		if isEligible {
			eligible = append(eligible, item)
			continue
		}
		switch reason {
		case cleaner.EligibilityReasonFiltered:
			summary.FilterCount++
			summary.FilterSize += item.Size
		case cleaner.EligibilityReasonRisky:
			summary.RiskyCount++
			summary.RiskySize += item.Size
		case cleaner.EligibilityReasonActiveWorktree:
			summary.ActiveCount++
			summary.ActiveSize += item.Size
		case cleaner.EligibilityReasonAge:
			summary.AgeCount++
			summary.AgeSize += item.Size
		case cleaner.EligibilityReasonAgentStateLive:
			summary.AgentStateLiveCount++
			summary.AgentStateLiveSize += item.Size
		case cleaner.EligibilityReasonAgentStateUndetermined:
			summary.AgentStateUndeterminedCount++
			summary.AgentStateUndeterminedSize += item.Size
		default:
			if summary.OtherBlocked == nil {
				summary.OtherBlocked = make(map[cleaner.EligibilityReason]cleanupDiagnosticBucket)
			}
			bucket := summary.OtherBlocked[reason]
			bucket.Count++
			bucket.Size += item.Size
			summary.OtherBlocked[reason] = bucket
		}
	}
	// clean applies the same existence filter and target normalization before
	// planning an execution, so the estimate counts each physical deletion
	// once: canonical aliases dedupe, eligible children nested inside an
	// eligible parent collapse to the parent, and vanished paths drop out.
	// Remaining clean-time safety protections (git safety, overlap safety,
	// scan-evidence filtering, physical owner checks) can only reduce the final
	// plan, which is why the figure is labelled an estimate.
	planned := cleaner.NormalizeTargets(cleaner.FilterExistingTargets(eligible))
	for _, target := range planned {
		summary.EligibleCount++
		summary.EligibleSize += target.Size
	}
	return summary
}

func printCleanupDiagnostics(summary cleanupDiagnostics, opts types.PruneOptions) {
	if summary.ActiveCount > 0 {
		fmt.Printf("  protected   %s active worktrees; use --include-active-worktrees after review\n",
			cleaner.FormatSize(summary.ActiveSize))
	}
	if summary.AgeCount > 0 {
		fmt.Printf("  age-blocked %s younger than %s\n",
			cleaner.FormatSize(summary.AgeSize), cleanAgeDisplay(opts.Age))
	}
	if summary.RiskyCount > 0 {
		fmt.Printf("  risky       %s requires --risky\n", cleaner.FormatSize(summary.RiskySize))
	}
	if summary.FilterCount > 0 && (len(opts.Categories) > 0 || len(opts.Tools) > 0) {
		fmt.Printf("  filtered    %s outside category/tool filters\n", cleaner.FormatSize(summary.FilterSize))
	}
	if summary.AgentStateLiveCount > 0 {
		fmt.Printf("  agent-state %s %s\n",
			cleaner.FormatSize(summary.AgentStateLiveSize), cleaner.EligibilityReasonAgentStateLive)
	}
	if summary.AgentStateUndeterminedCount > 0 {
		fmt.Printf("  agent-state %s %s\n",
			cleaner.FormatSize(summary.AgentStateUndeterminedSize), cleaner.EligibilityReasonAgentStateUndetermined)
	}
	if len(summary.OtherBlocked) > 0 {
		reasons := make([]string, 0, len(summary.OtherBlocked))
		for reason := range summary.OtherBlocked {
			reasons = append(reasons, string(reason))
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			bucket := summary.OtherBlocked[cleaner.EligibilityReason(reason)]
			fmt.Printf("  blocked     %s %s\n", cleaner.FormatSize(bucket.Size), reason)
		}
	}
}

func cleanAgeDisplay(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}

func itemName(item types.DebrisInfo) string {
	if item.Category == types.CategoryWorktree && item.Tool == types.ToolUnknown && item.Source != "" {
		return item.Source + "/" + item.ID
	}
	if item.ID != "" {
		return item.ID
	}
	return string(item.Tool)
}

func itemProject(item types.DebrisInfo) string {
	if item.Project != "" {
		return item.Project
	}
	switch item.Category {
	case types.CategoryBuildCache, types.CategoryOtherCache, types.CategoryAILogs:
		return "global"
	default:
		return "-"
	}
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
	if w.Reason != "" {
		return w.Reason
	}
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
	case types.CategoryAgentState:
		switch w.Classification {
		case types.EntryClassLive:
			return "recorded cwd exists"
		case types.EntryClassOrphaned:
			return "recorded cwd does not exist"
		default:
			return "recorded cwd could not be determined"
		}
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
	scanCmd.Flags().BoolVar(&scanDiagnostics, "diagnostics", false, "Report per-provider timing and diagnostics (experimental)")
	scanCmd.Flags().StringArrayVar(&scanRoots, "root", nil, "Scan root under $HOME (repeatable)")
	scanCmd.Flags().StringArrayVar(&scanExcludes, "exclude", nil, "Exclude a path or glob pattern under scan roots from discovery (repeatable)")
}
