package adapter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestCWDJSONExtractorLivesApartFromRecordedCWDReader(t *testing.T) {
	extractorNames := []string{
		"feed",
		"startKeyString",
		"startValue",
		"feedStringByte",
		"closeContainer",
		"finishValue",
		"feedNumberByte",
		"numberCanEnd",
		"matchKeyByte",
		"isJSONValueDelimiter",
		"isJSONHexDigit",
		"isJSONWhitespace",
	}
	readerNames := []string{
		"readRecordedCWDs",
		"reset",
		"unverifiableRecord",
	}

	wanted := make(map[string]string, len(extractorNames)+len(readerNames))
	for _, name := range extractorNames {
		wanted[name] = "claude_cwd_json_extractor.go"
	}
	for _, name := range readerNames {
		wanted[name] = "claude_cwd_json.go"
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
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}
