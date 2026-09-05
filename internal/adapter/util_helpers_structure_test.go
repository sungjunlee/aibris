package adapter

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package adapter after the same-package extract.
var (
	_ = estimateDirSize
	_ = estimateDirActivity
	_ = estimateDirActivityWithOptions
	_ = estimateDirSizes
	_ = estimateDirSizesWithDU
	_ = walkDirSequential
	_ = (*dirActivityAccumulator).recordModTime
	_ = (*dirActivityAccumulator).latestModTime
	_ = EstimateDirSize
	_ = NewestTreeModTime
	_ = IsWithin
	_ = UncoveredCodexHomeWarning
)

func TestUtilHelpersLiveApartFromPublicUtilEntry(t *testing.T) {
	helperNames := []string{
		"estimateDirSize",
		"estimateDirActivity",
		"estimateDirActivityWithOptions",
		"estimateDirSizes",
		"estimateDirSizesWithDU",
		"walkDirSequential",
		"recordModTime",
		"latestModTime",
		"dirActivity",
		"dirActivityAccumulator",
	}
	facadeNames := []string{
		"EstimateDirSize",
		"NewestTreeModTime",
		"detectProjectName",
		"projectNameFromRecordedCWD",
		"isHiddenDir",
		"scanRootsOrHome",
		"IsWithin",
		"pathUnderRoots",
		"applyCodexHomeScanRoots",
		"explicitScan",
		"isDefaultHomeScan",
		"UncoveredCodexHomeWarning",
		"appendUncoveredCodexHomes",
		"uncoveredCodexHomes",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "util_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "util.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse adapter: %v", err)
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

func TestUtilHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing adapter callers still resolve to the helper implementations.
	helpers := []any{
		estimateDirSize,
		estimateDirActivity,
		estimateDirActivityWithOptions,
		estimateDirSizes,
		estimateDirSizesWithDU,
		walkDirSequential,
		(*dirActivityAccumulator).recordModTime,
		(*dirActivityAccumulator).latestModTime,
	}
	public := []any{
		EstimateDirSize,
		NewestTreeModTime,
		IsWithin,
		UncoveredCodexHomeWarning,
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
		_ func(context.Context, string) int64                                         = estimateDirSize
		_ func(context.Context, string) dirActivity                                   = estimateDirActivity
		_ func(context.Context, string, bool) dirActivity                             = estimateDirActivityWithOptions
		_ func(context.Context, []string) map[string]int64                            = estimateDirSizes
		_ func(context.Context, []string) (map[string]int64, bool)                    = estimateDirSizesWithDU
		_ func(context.Context, string, *atomic.Int64, *dirActivityAccumulator, bool) = walkDirSequential
		_ func(*dirActivityAccumulator, time.Time)                                    = (*dirActivityAccumulator).recordModTime
		_ func(*dirActivityAccumulator, time.Time) time.Time                          = (*dirActivityAccumulator).latestModTime
		_ func(context.Context, string) int64                                         = EstimateDirSize
		_ func(context.Context, string) time.Time                                     = NewestTreeModTime
		_ func(string, string) bool                                                   = IsWithin
		_ func(types.ScanOptions) (string, error)                                     = UncoveredCodexHomeWarning
	)

	utilSource := readAdapterSource(t, "util.go")
	if !strings.Contains(utilSource, "func EstimateDirSize(") {
		t.Error("EstimateDirSize is not defined in util.go")
	}
	if !strings.Contains(utilSource, "func NewestTreeModTime(") {
		t.Error("NewestTreeModTime is not defined in util.go")
	}
	if !strings.Contains(utilSource, "estimateDirSize(") {
		t.Error("util.go no longer delegates to estimateDirSize")
	}
	if !strings.Contains(utilSource, "estimateDirActivity(") {
		t.Error("util.go no longer delegates to estimateDirActivity")
	}
	for _, name := range []string{
		"estimateDirSize",
		"estimateDirActivity",
		"estimateDirActivityWithOptions",
		"estimateDirSizes",
		"estimateDirSizesWithDU",
		"walkDirSequential",
		"recordModTime",
		"latestModTime",
	} {
		if strings.Contains(utilSource, "func "+name+"(") {
			t.Errorf("%s is still defined in util.go", name)
		}
	}
}

func readAdapterSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
