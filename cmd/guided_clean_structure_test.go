package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestGuidedCleanRenderLivesApartFromPolicyState(t *testing.T) {
	renderNames := []string{
		"promptGuidedCleanForFiles",
		"promptGuidedCleanStateForFiles",
		"promptGuidedClean",
		"promptGuidedCleanWithMode",
		"promptGuidedCleanStateWithMode",
		"renderGuidedClean",
		"renderGuidedRows",
	}
	policyNames := []string{
		"buildGuidedCleanState",
		"planGuidedCleanState",
		"toggleGuidedCleanRow",
		"applyGuidedCleanCommand",
		"adjustGuidedCleanAge",
		"replanGuidedCleanAge",
	}

	wanted := make(map[string]string, len(renderNames)+len(policyNames))
	for _, name := range renderNames {
		wanted[name] = "guided_clean_render.go"
	}
	for _, name := range policyNames {
		wanted[name] = "guided_clean.go"
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
}
