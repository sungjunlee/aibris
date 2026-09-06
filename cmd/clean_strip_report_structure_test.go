package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = printStripCWDRefusals
	_ = printStripUnitOutcome
	_ = printStripSubtreeLine
	_ = summarizeStripOutcomes
	_ = accountStripUnit
	_ = recordStripKeeps
	_ = sortedStripReasons
	_ = printStripCloser
	_ = unitNoun
	_ = printStripPlan
	_ = printStripOutcomes
	_ = runStripClean
)

func TestStripReportHelpersLiveApartFromStripFacade(t *testing.T) {
	helperNames := []string{
		"printStripCWDRefusals",
		"printStripUnitOutcome",
		"printStripSubtreeLine",
		"summarizeStripOutcomes",
		"accountStripUnit",
		"recordStripKeeps",
		"sortedStripReasons",
		"printStripCloser",
		"unitNoun",
	}
	helperTypes := []string{
		"stripCloser",
	}
	facadeNames := []string{
		"runStripClean",
		"printStripPlan",
		"printStripOutcomes",
	}

	wanted := make(map[string]string, len(helperNames)+len(helperTypes)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "clean_strip_report.go"
	}
	for _, name := range helperTypes {
		wanted[name] = "clean_strip_report.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "clean_strip.go"
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
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv != nil {
						continue
					}
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

func TestStripReportHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func([]types.DebrisInfo, string)                                 = printStripCWDRefusals
		_ func(stripUnitOutcome)                                           = printStripUnitOutcome
		_ func(stripSubtreeOutcome)                                        = printStripSubtreeLine
		_ func([]stripUnitOutcome, int) stripCloser                        = summarizeStripOutcomes
		_ func(stripUnitOutcome, *stripCloser, map[string]struct{})        = accountStripUnit
		_ func(stripUnitOutcome, map[string]struct{}) bool                 = recordStripKeeps
		_ func(map[string]struct{}) []string                               = sortedStripReasons
		_ func(stripCloser)                                                = printStripCloser
		_ func(int) string                                                 = unitNoun
		_ func([]types.DebrisInfo, []types.DebrisInfo, types.PruneOptions) = printStripPlan
		_ func([]stripUnitOutcome, int)                                    = printStripOutcomes
	)

	facadeSource := readCmdSource(t, "clean_strip.go")
	reportSource := readCmdSource(t, "clean_strip_report.go")
	if !strings.Contains(facadeSource, "func runStripClean(") {
		t.Error("runStripClean is not defined in clean_strip.go")
	}
	if !strings.Contains(facadeSource, "func printStripPlan(") {
		t.Error("printStripPlan is not defined in clean_strip.go")
	}
	if !strings.Contains(facadeSource, "func printStripOutcomes(") {
		t.Error("printStripOutcomes is not defined in clean_strip.go")
	}
	if !strings.Contains(facadeSource, "printStripCWDRefusals(") {
		t.Error("clean_strip.go no longer delegates to printStripCWDRefusals")
	}
	if !strings.Contains(facadeSource, "printStripUnitOutcome(") {
		t.Error("clean_strip.go no longer delegates to printStripUnitOutcome")
	}
	if !strings.Contains(facadeSource, "printStripCloser(") {
		t.Error("clean_strip.go no longer delegates to printStripCloser")
	}
	if !strings.Contains(facadeSource, "summarizeStripOutcomes(") {
		t.Error("clean_strip.go no longer delegates to summarizeStripOutcomes")
	}
	for _, name := range []string{
		"printStripCWDRefusals",
		"printStripUnitOutcome",
		"printStripSubtreeLine",
		"summarizeStripOutcomes",
		"accountStripUnit",
		"recordStripKeeps",
		"sortedStripReasons",
		"printStripCloser",
		"unitNoun",
	} {
		if strings.Contains(facadeSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_strip.go", name)
		}
		if !strings.Contains(reportSource, "func "+name+"(") {
			t.Errorf("%s is not defined in clean_strip_report.go", name)
		}
	}
	if strings.Contains(facadeSource, "type stripCloser ") || strings.Contains(facadeSource, "type stripCloser struct") {
		t.Error("stripCloser is still defined in clean_strip.go")
	}
	if !strings.Contains(reportSource, "type stripCloser struct") {
		t.Error("stripCloser is not defined in clean_strip_report.go")
	}
}
