package cmd

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = cachedScanMissReason
	_ = printLastScanReuse
	_ = printLastScanRescan
	_ = scanResultHasExclusions
	_ = loadLastScanSession
	_ = writeLastScanCache
	_ = inspectLastScanCache
)

func TestScanCacheHelpersLiveApartFromScanCacheCommandEntry(t *testing.T) {
	helperNames := []string{
		"cachedScanMissReason",
		"printLastScanReuse",
		"printLastScanRescan",
		"scanResultHasExclusions",
		"tryCached",
		"excludedReason",
		"liveScan",
		"runLive",
	}
	entryNames := []string{
		"loadLastScanSession",
		"writeLastScanCache",
		"inspectLastScanCache",
	}
	helperTypes := []string{
		"lastScanSession",
	}
	entryTypes := []string{
		"lastScanCache",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames)+len(helperTypes)+len(entryTypes))
	for _, name := range helperNames {
		wanted[name] = "scan_cache_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "scan_cache.go"
	}
	for _, name := range helperTypes {
		wanted[name] = "scan_cache_helpers.go"
	}
	for _, name := range entryTypes {
		wanted[name] = "scan_cache.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cmd: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if _, ok := wanted[d.Name.Name]; !ok {
						continue
					}
					owners[d.Name.Name] = append(owners[d.Name.Name], base)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						if _, ok := wanted[ts.Name.Name]; !ok {
							continue
						}
						owners[ts.Name.Name] = append(owners[ts.Name.Name], base)
					}
				}
			}
		}
	}

	for name, owner := range wanted {
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestScanCacheHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func(bool, string) string                                                                            = cachedScanMissReason
		_ func(time.Duration, bool)                                                                            = printLastScanReuse
		_ func(string, bool)                                                                                   = printLastScanRescan
		_ func(*types.ScanResult) bool                                                                         = scanResultHasExclusions
		_ func(context.Context, []string, []string, string, bool, bool) (*types.ScanResult, scanSource, error) = loadLastScanSession
		_ func([]string, string, *types.ScanResult, bool)                                                      = writeLastScanCache
		_ func([]string, string, bool) (*types.ScanResult, time.Duration, string, bool)                        = inspectLastScanCache
		_ func(context.Context) (*types.ScanResult, scanSource, error)                                         = (lastScanSession{}).load
	)

	cacheSource := readCmdSource(t, "scan_cache.go")
	if !strings.Contains(cacheSource, "func loadLastScanSession(") {
		t.Error("loadLastScanSession is not defined in scan_cache.go")
	}
	if !strings.Contains(cacheSource, "func writeLastScanCache(") {
		t.Error("writeLastScanCache is not defined in scan_cache.go")
	}
	if !strings.Contains(cacheSource, "lastScanSession{") {
		t.Error("scan_cache.go no longer delegates to lastScanSession")
	}
	if !strings.Contains(cacheSource, ".load(") {
		t.Error("scan_cache.go no longer delegates to lastScanSession.load")
	}
	for _, name := range []string{
		"cachedScanMissReason",
		"printLastScanReuse",
		"printLastScanRescan",
		"scanResultHasExclusions",
	} {
		if strings.Contains(cacheSource, "func "+name+"(") {
			t.Errorf("%s is still defined in scan_cache.go", name)
		}
	}
}
