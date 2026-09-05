package cmd

import (
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
	_ = confirmCleanExecution
	_ = parseAge
	_ = printCleanHeader
	_ = shortDurationString
	_ = printCleanCandidateSummary
	_ = candidateNoun
)

func TestCleanHelpersLiveApartFromCleanCommandEntry(t *testing.T) {
	helperNames := []string{
		"confirmCleanExecution",
		"parseAge",
		"printCleanHeader",
		"shortDurationString",
		"printCleanCandidateSummary",
		"candidateNoun",
	}
	entryNames := []string{
		"cleanCmd",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "clean_helpers.go"
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

func TestCleanHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func() bool                         = confirmCleanExecution
		_ func(string) (time.Duration, error) = parseAge
		_ func([]string)                      = printCleanHeader
		_ func(time.Duration) string          = shortDurationString
		_ func([]types.DebrisInfo)            = printCleanCandidateSummary
		_ func(int) string                    = candidateNoun
	)

	cleanSource := readCmdSource(t, "clean.go")
	if !strings.Contains(cleanSource, "var cleanCmd") {
		t.Error("cleanCmd is not defined in clean.go")
	}
	for _, name := range []string{
		"confirmCleanExecution",
		"parseAge",
		"printCleanHeader",
		"printCleanCandidateSummary",
	} {
		if strings.Contains(cleanSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean.go", name)
		}
		if !strings.Contains(cleanSource, name+"(") {
			t.Errorf("clean.go no longer delegates to %s", name)
		}
	}
	for _, name := range []string{
		"shortDurationString",
		"candidateNoun",
	} {
		if strings.Contains(cleanSource, "func "+name+"(") {
			t.Errorf("%s is still defined in clean.go", name)
		}
	}
}
