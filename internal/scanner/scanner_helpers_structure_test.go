package scanner

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package scanner after the same-package extract.
var (
	_ = applyUserExclusions
	_ = scanRetention
	_ = Scan
	_ = ScanWithOptions
	_ = DefaultScanOptions
	_ = New
	_ = NewWithRetentionProviders
	_ = emitProgress
	_ = totalSize
	_ = (*Scanner).dispatchProviders
	_ = (*Scanner).aggregateProviderResults
)

func TestScannerHelpersLiveApartFromScanEntry(t *testing.T) {
	helperNames := []string{
		"applyUserExclusions",
		"scanRetention",
		"emitProgress",
		"totalSize",
	}
	facadeNames := []string{
		"Scan",
		"ScanWithOptions",
		"New",
		"NewWithRetentionProviders",
		"DefaultScanOptions",
		"ProviderIdentity",
	}
	dispatchNames := []string{
		"dispatchProviders",
	}
	aggregateNames := []string{
		"aggregateProviderResults",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames)+len(dispatchNames)+len(aggregateNames))
	for _, name := range helperNames {
		wanted[name] = "scanner_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "scanner.go"
	}
	for _, name := range dispatchNames {
		wanted[name] = "scanner_dispatch.go"
	}
	for _, name := range aggregateNames {
		wanted[name] = "scanner_aggregate.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse scanner: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if _, ok := wanted[fn.Name.Name]; !ok {
					continue
				}
				owners[fn.Name.Name] = append(owners[fn.Name.Name], base)
			}
		}
	}

	for name, owner := range wanted {
		files := uniqueStrings(owners[name])
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestScannerHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing scanner callers still resolve to the helper implementations.
	helpers := []any{
		applyUserExclusions,
		scanRetention,
		emitProgress,
		totalSize,
		(*Scanner).dispatchProviders,
		(*Scanner).aggregateProviderResults,
	}
	public := []any{
		Scan,
		ScanWithOptions,
		DefaultScanOptions,
		New,
		NewWithRetentionProviders,
	}
	for i, fn := range helpers {
		if fn == nil {
			t.Errorf("helper %d is nil", i)
		}
	}
	for i, fn := range public {
		if fn == nil {
			t.Errorf("public %d is nil", i)
		}
	}

	var (
		_ func(*types.ScanResult, types.ScanOptions)                                                                         = applyUserExclusions
		_ func(context.Context, types.ScanOptions, []types.RetentionProvider) types.RetentionProjection                      = scanRetention
		_ func(func(types.ScanProgressEvent), types.ScanProgressEvent)                                                       = emitProgress
		_ func([]types.DebrisInfo) int64                                                                                     = totalSize
		_ func(context.Context) (*types.ScanResult, error)                                                                   = Scan
		_ func(context.Context, types.ScanOptions) (*types.ScanResult, error)                                                = ScanWithOptions
		_ func() (types.ScanOptions, error)                                                                                  = DefaultScanOptions
		_ func([]adapter.DebrisProvider) *Scanner                                                                            = New
		_ func([]adapter.DebrisProvider, []types.RetentionProvider) *Scanner                                                 = NewWithRetentionProviders
		_ func(*Scanner) string                                                                                              = (*Scanner).ProviderIdentity
		_ func(*Scanner, context.Context) (*types.ScanResult, error)                                                         = (*Scanner).Scan
		_ func(*Scanner, context.Context, types.ScanOptions) (*types.ScanResult, error)                                      = (*Scanner).ScanWithOptions
		_ func(*Scanner, context.Context, context.CancelFunc, types.ScanOptions) <-chan providerScanResult                   = (*Scanner).dispatchProviders
		_ func(*Scanner, context.Context, types.ScanOptions, []string, <-chan providerScanResult) (*types.ScanResult, error) = (*Scanner).aggregateProviderResults
	)

	scannerSource := readScannerSource(t, "scanner.go")
	if !strings.Contains(scannerSource, "func (s *Scanner) Scan(") {
		t.Error("Scan is not defined in scanner.go")
	}
	if !strings.Contains(scannerSource, "func (s *Scanner) ScanWithOptions(") {
		t.Error("ScanWithOptions is not defined in scanner.go")
	}
	if !strings.Contains(scannerSource, "func Scan(") {
		t.Error("package Scan is not defined in scanner.go")
	}
	if !strings.Contains(scannerSource, "func DefaultScanOptions(") {
		t.Error("DefaultScanOptions is not defined in scanner.go")
	}
	for _, name := range []string{
		"applyUserExclusions",
		"scanRetention",
		"emitProgress",
		"totalSize",
		"dispatchProviders",
		"aggregateProviderResults",
	} {
		if strings.Contains(scannerSource, "func "+name+"(") || strings.Contains(scannerSource, "func (s *Scanner) "+name+"(") {
			t.Errorf("%s is still defined in scanner.go", name)
		}
	}
	if !strings.Contains(scannerSource, "dispatchProviders(") {
		t.Error("scanner.go no longer delegates to dispatchProviders")
	}
	if !strings.Contains(scannerSource, "aggregateProviderResults(") {
		t.Error("scanner.go no longer delegates to aggregateProviderResults")
	}

	dispatchSource := readScannerSource(t, "scanner_dispatch.go")
	if !strings.Contains(dispatchSource, "func (s *Scanner) dispatchProviders(") {
		t.Error("dispatchProviders is not defined in scanner_dispatch.go")
	}
	if !strings.Contains(dispatchSource, "emitProgress(") {
		t.Error("scanner_dispatch.go no longer emits provider start progress")
	}

	aggregateSource := readScannerSource(t, "scanner_aggregate.go")
	if !strings.Contains(aggregateSource, "func (s *Scanner) aggregateProviderResults(") {
		t.Error("aggregateProviderResults is not defined in scanner_aggregate.go")
	}
	for _, name := range []string{
		"applyUserExclusions",
		"scanRetention",
		"totalSize",
	} {
		if !strings.Contains(aggregateSource, name+"(") {
			t.Errorf("scanner_aggregate.go no longer delegates to %s", name)
		}
	}
}

func TestScanWithOptionsDoesNotContainDispatchOrAggregationBodies(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "scanner.go", nil, 0)
	if err != nil {
		t.Fatalf("parse scanner.go: %v", err)
	}

	scanWithOptions := scannerMethod(file, "ScanWithOptions")
	if scanWithOptions == nil {
		t.Fatal("ScanWithOptions method is not defined in scanner.go")
	}

	forbidden := map[string]string{
		"startGate":               "dispatch start gate",
		"sem":                     "dispatch semaphore",
		"maxParallelProviders":    "dispatch parallelism cap",
		"providerScanResult":      "provider result type",
		"applyUserExclusions":     "aggregation user exclusions",
		"fillInventoryTotals":     "aggregation inventory totals",
		"scanRetention":           "aggregation retention",
		"requireTempDirOwnership": "aggregation ownership gate",
		"emitProgress":            "provider progress emission",
		"totalSize":               "per-provider size totals",
	}
	var leaked []string
	var goStmts int
	called := map[string]int{}
	ast.Inspect(scanWithOptions.Body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.GoStmt:
			goStmts++
		case *ast.Ident:
			if reason, ok := forbidden[n.Name]; ok {
				leaked = append(leaked, reason+" ("+n.Name+")")
			}
			if n.Name == "dispatchProviders" || n.Name == "aggregateProviderResults" {
				called[n.Name]++
			}
		}
		return true
	})
	if goStmts > 0 {
		t.Errorf("ScanWithOptions contains %d go statement(s); dispatch loop leaked back", goStmts)
	}
	for _, reason := range uniqueStrings(leaked) {
		t.Errorf("ScanWithOptions body leaked %s", reason)
	}
	if called["dispatchProviders"] != 1 {
		t.Errorf("ScanWithOptions calls dispatchProviders %d times; want 1", called["dispatchProviders"])
	}
	if called["aggregateProviderResults"] != 1 {
		t.Errorf("ScanWithOptions calls aggregateProviderResults %d times; want 1", called["aggregateProviderResults"])
	}

	dispatchFile, err := parser.ParseFile(fset, "scanner_dispatch.go", nil, 0)
	if err != nil {
		t.Fatalf("parse scanner_dispatch.go: %v", err)
	}
	dispatch := scannerMethod(dispatchFile, "dispatchProviders")
	if dispatch == nil {
		t.Fatal("dispatchProviders is not defined in scanner_dispatch.go")
	}
	dispatchIdents := identSet(dispatch.Body)
	if goStmtCount(dispatch.Body) == 0 {
		t.Error("dispatchProviders no longer owns the provider goroutine loop")
	}
	for _, name := range []string{"startGate", "sem", "maxParallelProviders"} {
		if !dispatchIdents[name] {
			t.Errorf("dispatchProviders no longer uses %s", name)
		}
	}

	aggregateFile, err := parser.ParseFile(fset, "scanner_aggregate.go", nil, 0)
	if err != nil {
		t.Fatalf("parse scanner_aggregate.go: %v", err)
	}
	aggregate := scannerMethod(aggregateFile, "aggregateProviderResults")
	if aggregate == nil {
		t.Fatal("aggregateProviderResults is not defined in scanner_aggregate.go")
	}
	if rangeStmtCount(aggregate.Body) == 0 {
		t.Error("aggregateProviderResults no longer owns the provider result loop")
	}
	aggregateIdents := identSet(aggregate.Body)
	for _, name := range []string{
		"applyUserExclusions",
		"fillInventoryTotals",
		"scanRetention",
		"requireTempDirOwnership",
	} {
		if !aggregateIdents[name] {
			t.Errorf("aggregateProviderResults no longer calls %s", name)
		}
	}
}

func scannerMethod(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || fn.Name.Name != name {
			continue
		}
		return fn
	}
	return nil
}

func identSet(body *ast.BlockStmt) map[string]bool {
	seen := make(map[string]bool)
	ast.Inspect(body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			seen[ident.Name] = true
		}
		return true
	})
	return seen
}

func goStmtCount(body *ast.BlockStmt) int {
	var count int
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.GoStmt); ok {
			count++
		}
		return true
	})
	return count
}

func rangeStmtCount(body *ast.BlockStmt) int {
	var count int
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.RangeStmt); ok {
			count++
		}
		return true
	})
	return count
}

func readScannerSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
