package scanreport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

// Compile-time re-export identity: the original helper names still resolve
// in package scanreport after the same-package extract.
var (
	_ = WriteHuman
	_ = writeScanHeadline
	_ = WriteHeadline
	_ = writePressureHint
	_ = writeDefaultCacheRelaxNote
	_ = WriteNext
	_ = WriteReviewOnlyLine
	_ = reviewOnlyNoun
	_ = writeReclaimLadder
	_ = WriteVolumePressure
	_ = WriteHumanExclusions
	_ = excludeSourceCounts
	_ = resolvedDisplayHome
	_ = DisplayHomePath
	_ = WriteRetention
	_ = writeDiagnostics
	_ = writeCodexActivity
	_ = codexWorktreeNoun
	_ = writeCategorySummary
	_ = writeLargestItems
	_ = sortedCategories
	_ = WriteCleanupDiagnostics
	_ = itemName
	_ = itemProject
	_ = itemAgeAndStatus
)

func TestHumanHelpersLiveApartFromPublicHumanRenderEntry(t *testing.T) {
	helperNames := []string{
		"writeScanHeadline",
		"WriteHeadline",
		"writePressureHint",
		"writeDefaultCacheRelaxNote",
		"WriteNext",
		"WriteReviewOnlyLine",
		"reviewOnlyNoun",
		"writeReclaimLadder",
		"WriteVolumePressure",
		"WriteHumanExclusions",
		"excludeSourceCounts",
		"resolvedDisplayHome",
		"DisplayHomePath",
		"WriteRetention",
		"writeDiagnostics",
		"writeCodexActivity",
		"codexWorktreeNoun",
		"writeCategorySummary",
		"writeLargestItems",
		"sortedCategories",
		"WriteCleanupDiagnostics",
		"itemName",
		"itemProject",
		"itemAgeAndStatus",
	}
	facadeNames := []string{
		"WriteHuman",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "human_helpers.go"
	}
	for _, name := range itemHelperNames() {
		wanted[name] = "human_items.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "human.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse scanreport: %v", err)
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

func TestHumanHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing scanreport callers still resolve to the helper implementations.
	helpers := []any{
		writeScanHeadline,
		WriteHeadline,
		writePressureHint,
		writeDefaultCacheRelaxNote,
		WriteNext,
		WriteReviewOnlyLine,
		reviewOnlyNoun,
		writeReclaimLadder,
		WriteVolumePressure,
		WriteHumanExclusions,
		excludeSourceCounts,
		resolvedDisplayHome,
		DisplayHomePath,
		WriteRetention,
		writeDiagnostics,
		writeCodexActivity,
		codexWorktreeNoun,
		writeCategorySummary,
		writeLargestItems,
		sortedCategories,
		WriteCleanupDiagnostics,
		itemName,
		itemProject,
		itemAgeAndStatus,
	}
	public := []any{
		WriteHuman,
		WriteHeadline,
		WriteNext,
		WriteReviewOnlyLine,
		WriteVolumePressure,
		WriteHumanExclusions,
		DisplayHomePath,
		WriteRetention,
		WriteCleanupDiagnostics,
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
		_ func(io.Writer, View)                                           = WriteHuman
		_ func(io.Writer, int64, []ReclaimPath, *volume.Report)           = WriteHeadline
		_ func(io.Writer, View)                                           = WriteNext
		_ func(io.Writer, int, int64)                                     = WriteReviewOnlyLine
		_ func(io.Writer, *volume.Report)                                 = WriteVolumePressure
		_ func(io.Writer, View)                                           = WriteHumanExclusions
		_ func(string, string) string                                     = DisplayHomePath
		_ func(io.Writer, types.RetentionProjection)                      = WriteRetention
		_ func(io.Writer, CleanupProjection, types.PruneOptions)          = WriteCleanupDiagnostics
		_ func(io.Writer, types.PruneOptions)                             = writeDefaultCacheRelaxNote
		_ func(int) string                                                = reviewOnlyNoun
		_ func(View) (int, int)                                           = excludeSourceCounts
		_ func(string) string                                             = resolvedDisplayHome
		_ func(int) string                                                = codexWorktreeNoun
		_ func(map[types.Category]types.CategorySummary) []types.Category = sortedCategories
		_ func(Item) string                                               = itemName
		_ func(Item) string                                               = itemProject
		_ func(Item) string                                               = itemAgeAndStatus
	)

	humanSource := readScanreportSource(t, "human.go")
	helperSource := readScanreportSource(t, "human_helpers.go")
	itemSource := readScanreportSource(t, "human_items.go")
	if !strings.Contains(humanSource, "func WriteHuman(") {
		t.Error("WriteHuman is not defined in human.go")
	}
	if strings.Contains(helperSource, "func WriteHuman(") {
		t.Error("WriteHuman moved out of the public human-render entry")
	}
	moved := make(map[string]bool)
	for _, name := range itemHelperNames() {
		moved[name] = true
	}
	for _, name := range []string{
		"writeScanHeadline",
		"WriteHeadline",
		"writePressureHint",
		"writeDefaultCacheRelaxNote",
		"WriteNext",
		"WriteReviewOnlyLine",
		"reviewOnlyNoun",
		"writeReclaimLadder",
		"WriteVolumePressure",
		"WriteHumanExclusions",
		"excludeSourceCounts",
		"resolvedDisplayHome",
		"DisplayHomePath",
		"WriteRetention",
		"writeDiagnostics",
		"writeCodexActivity",
		"codexWorktreeNoun",
		"writeCategorySummary",
		"writeLargestItems",
		"sortedCategories",
		"WriteCleanupDiagnostics",
		"itemName",
		"itemProject",
		"itemAgeAndStatus",
	} {
		if strings.Contains(humanSource, "func "+name+"(") {
			t.Errorf("%s is still defined in human.go", name)
		}
		if moved[name] {
			if !strings.Contains(itemSource, "func "+name+"(") {
				t.Errorf("%s is not defined in human_items.go", name)
			}
			if strings.Contains(helperSource, "func "+name+"(") {
				t.Errorf("%s is still defined in human_helpers.go", name)
			}
			continue
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in human_helpers.go", name)
		}
	}
	for _, name := range []string{
		"writeScanHeadline",
		"WriteVolumePressure",
		"WriteCleanupDiagnostics",
		"writeDefaultCacheRelaxNote",
		"WriteHumanExclusions",
		"writeCategorySummary",
		"writeLargestItems",
		"WriteRetention",
		"writeCodexActivity",
		"writeDiagnostics",
		"WriteNext",
	} {
		if !strings.Contains(humanSource, name+"(") {
			t.Errorf("human.go no longer delegates to %s", name)
		}
	}
}

// itemHelperNames lists the item-display helper cluster extracted to
// human_items.go alongside the remaining human_helpers.go printers.
func itemHelperNames() []string {
	return []string{
		"writeCategorySummary",
		"writeLargestItems",
		"sortedCategories",
		"itemName",
		"itemProject",
		"itemAgeAndStatus",
	}
}

func readScanreportSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
