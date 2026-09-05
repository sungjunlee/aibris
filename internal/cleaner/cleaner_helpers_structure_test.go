package cleaner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cleaner after the same-package extract.
var (
	_ = IsSafePath
	_ = IsSafeTarget
	_ = goBuildCacheTarget
	_ = safeHomeRel
	_ = containsCategory
	_ = containsTool
	_ = FormatSize
	_ = Filter
	_ = Execute
)

func TestCleanerHelpersLiveApartFromExecuteEntry(t *testing.T) {
	helperNames := []string{
		"IsSafePath",
		"IsSafeTarget",
		"goBuildCacheTarget",
		"safeHomeRel",
		"containsCategory",
		"containsTool",
		"FormatSize",
	}
	facadeNames := []string{
		"Filter",
		"Execute",
		"ExecuteWithContext",
		"ExecuteWithContextAndBarrier",
		"ExecuteWithContextAndBarrierWithOutput",
		"ExecuteWithContextAndBarrierWithOutputAndObserver",
		"executeWithContext",
		"executeWithContextOutput",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "cleaner_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "cleaner.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cleaner: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv != nil {
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
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestCleanerHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cleaner callers still resolve to the helper implementations.
	helpers := []any{
		IsSafePath,
		IsSafeTarget,
		goBuildCacheTarget,
		safeHomeRel,
		containsCategory,
		containsTool,
		FormatSize,
	}
	public := []any{
		IsSafePath,
		IsSafeTarget,
		Filter,
		Execute,
		FormatSize,
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
		_ func(string, string) bool                                       = IsSafePath
		_ func(string, types.DebrisInfo) bool                             = IsSafeTarget
		_ func([]types.DebrisInfo, types.PruneOptions) []types.DebrisInfo = Filter
		_ func([]types.DebrisInfo) (int64, error)                         = Execute
		_ func(int64) string                                              = FormatSize
	)

	cleanerSource := readCleanerSource(t, "cleaner.go")
	for _, name := range []string{
		"IsSafePath",
		"IsSafeTarget",
		"goBuildCacheTarget",
		"safeHomeRel",
		"containsCategory",
		"containsTool",
		"FormatSize",
	} {
		if strings.Contains(cleanerSource, "func "+name+"(") {
			t.Errorf("%s is still defined in cleaner.go", name)
		}
	}
	for _, name := range []string{
		"IsSafeTarget",
		"FormatSize",
	} {
		if !strings.Contains(cleanerSource, name+"(") {
			t.Errorf("cleaner.go no longer delegates to %s", name)
		}
	}
}

func readCleanerSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
