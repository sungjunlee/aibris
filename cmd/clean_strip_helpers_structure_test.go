package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripExecutionHelpersLiveApartFromStripFacade(t *testing.T) {
	helperNames := []string{
		"executeStripTargets",
		"stripWorktreeUnit",
		"skippedStripSubtrees",
		"stripCheckoutDir",
		"stripSubtreeGitSafe",
	}
	facadeNames := []string{
		"runStripClean",
		"selectStripTargets",
		"mergeStripEligibleByOwner",
		"printStripPlan",
		"printStripOutcomes",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "clean_strip_helpers.go"
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

	facadeSource := readCmdSource(t, "clean_strip.go")
	for _, name := range helperNames {
		if strings.Contains(facadeSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean_strip.go", name)
		}
	}
	if !strings.Contains(facadeSource, "executeStripTargets(") {
		t.Errorf("clean_strip.go no longer delegates to executeStripTargets")
	}
}
