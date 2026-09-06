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
)

func TestScannerHelpersLiveApartFromScanEntry(t *testing.T) {
	helperNames := []string{
		"applyUserExclusions",
		"scanRetention",
	}
	facadeNames := []string{
		"Scan",
		"ScanWithOptions",
		"New",
		"NewWithRetentionProviders",
		"DefaultScanOptions",
		"ProviderIdentity",
		"emitProgress",
		"totalSize",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "scanner_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "scanner.go"
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
		_ func(*types.ScanResult, types.ScanOptions)                                                    = applyUserExclusions
		_ func(context.Context, types.ScanOptions, []types.RetentionProvider) types.RetentionProjection = scanRetention
		_ func(context.Context) (*types.ScanResult, error)                                              = Scan
		_ func(context.Context, types.ScanOptions) (*types.ScanResult, error)                           = ScanWithOptions
		_ func() (types.ScanOptions, error)                                                             = DefaultScanOptions
		_ func([]adapter.DebrisProvider) *Scanner                                                       = New
		_ func([]adapter.DebrisProvider, []types.RetentionProvider) *Scanner                            = NewWithRetentionProviders
		_ func(*Scanner) string                                                                         = (*Scanner).ProviderIdentity
		_ func(*Scanner, context.Context) (*types.ScanResult, error)                                    = (*Scanner).Scan
		_ func(*Scanner, context.Context, types.ScanOptions) (*types.ScanResult, error)                 = (*Scanner).ScanWithOptions
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
	} {
		if strings.Contains(scannerSource, "func "+name+"(") {
			t.Errorf("%s is still defined in scanner.go", name)
		}
	}
	if !strings.Contains(scannerSource, "applyUserExclusions(") {
		t.Error("scanner.go no longer delegates to applyUserExclusions")
	}
	if !strings.Contains(scannerSource, "scanRetention(") {
		t.Error("scanner.go no longer delegates to scanRetention")
	}
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
