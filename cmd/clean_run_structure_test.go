package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = runCleanCommand
	_ = selectCleanCommandRoute
)

func TestCleanRunLivesApartFromCleanCommandEntry(t *testing.T) {
	runNames := []string{
		"runCleanCommand",
		"selectCleanCommandRoute",
	}
	entryNames := []string{
		"cleanCmd",
	}

	wanted := make(map[string]string, len(runNames)+len(entryNames))
	for _, name := range runNames {
		wanted[name] = "clean_run.go"
	}
	for _, name := range entryNames {
		wanted[name] = "clean.go"
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
					if d.Recv != nil {
						continue
					}
					if _, ok := wanted[d.Name.Name]; !ok {
						continue
					}
					owners[d.Name.Name] = append(owners[d.Name.Name], base)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for _, ident := range vs.Names {
							if _, ok := wanted[ident.Name]; !ok {
								continue
							}
							owners[ident.Name] = append(owners[ident.Name], base)
						}
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

func TestCleanRunReexportIdentity(t *testing.T) {
	var (
		_ func(*cobra.Command)                             = runCleanCommand
		_ func(*cobra.Command) (cleanCommandRoute, string) = selectCleanCommandRoute
	)

	cleanSource := readCmdSource(t, "clean.go")
	if !strings.Contains(cleanSource, "var cleanCmd") {
		t.Error("cleanCmd is not defined in clean.go")
	}
	if strings.Contains(cleanSource, "func runCleanCommand(") {
		t.Error("runCleanCommand is still defined in clean.go")
	}
	if !strings.Contains(cleanSource, "runCleanCommand(") {
		t.Error("clean.go no longer delegates to runCleanCommand")
	}

	runSource := readCmdSource(t, "clean_run.go")
	if !strings.Contains(runSource, "func runCleanCommand(") {
		t.Error("runCleanCommand is not defined in clean_run.go")
	}
	for _, name := range []string{
		"runStripClean",
		"runAPFSSnapshotClean",
		"runCleanJSON",
		"chooseCleanExperience",
	} {
		if strings.Contains(runSource, "func "+name+"(") {
			t.Errorf("%s was rewritten into clean_run.go", name)
		}
		if !strings.Contains(runSource, name+"(") {
			t.Errorf("clean_run.go no longer calls %s", name)
		}
	}
}
