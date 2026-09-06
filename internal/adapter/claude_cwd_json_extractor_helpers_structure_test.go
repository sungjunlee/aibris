package adapter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Compile-time re-export identity: the original helper names still resolve
// in package adapter after the same-package extract.
var (
	_ = (*cwdMetadataExtractor).feed
	_ = (*cwdMetadataExtractor).startKeyString
	_ = (*cwdMetadataExtractor).startValue
	_ = (*cwdMetadataExtractor).feedStringByte
	_ = (*cwdMetadataExtractor).closeContainer
	_ = (*cwdMetadataExtractor).finishValue
	_ = (*cwdMetadataExtractor).feedNumberByte
	_ = (*cwdMetadataExtractor).numberCanEnd
	_ = (*cwdMetadataExtractor).matchKeyByte
	_ = isJSONValueDelimiter
	_ = isJSONHexDigit
	_ = isJSONWhitespace
)

func TestCWDJSONExtractorHelpersLiveApartFromFeedEntry(t *testing.T) {
	helperNames := []string{
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
	entryNames := []string{
		"feed",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "claude_cwd_json_extractor_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "claude_cwd_json_extractor.go"
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
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
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

func TestCWDJSONExtractorHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing adapter callers still resolve to the helper implementations.
	helpers := []any{
		(*cwdMetadataExtractor).startKeyString,
		(*cwdMetadataExtractor).startValue,
		(*cwdMetadataExtractor).feedStringByte,
		(*cwdMetadataExtractor).closeContainer,
		(*cwdMetadataExtractor).finishValue,
		(*cwdMetadataExtractor).feedNumberByte,
		(*cwdMetadataExtractor).numberCanEnd,
		(*cwdMetadataExtractor).matchKeyByte,
		isJSONValueDelimiter,
		isJSONHexDigit,
		isJSONWhitespace,
	}
	public := []any{
		(*cwdMetadataExtractor).feed,
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
		_ func(*cwdMetadataExtractor, []byte) string = (*cwdMetadataExtractor).feed
		_ func(*cwdMetadataExtractor)                = (*cwdMetadataExtractor).startKeyString
		_ func(*cwdMetadataExtractor, byte)          = (*cwdMetadataExtractor).startValue
		_ func(*cwdMetadataExtractor, byte) string   = (*cwdMetadataExtractor).feedStringByte
		_ func(*cwdMetadataExtractor, byte)          = (*cwdMetadataExtractor).closeContainer
		_ func(*cwdMetadataExtractor)                = (*cwdMetadataExtractor).finishValue
		_ func(*cwdMetadataExtractor, byte) bool     = (*cwdMetadataExtractor).feedNumberByte
		_ func(*cwdMetadataExtractor) bool           = (*cwdMetadataExtractor).numberCanEnd
		_ func(*cwdMetadataExtractor, byte)          = (*cwdMetadataExtractor).matchKeyByte
		_ func(byte) bool                            = isJSONValueDelimiter
		_ func(byte) bool                            = isJSONHexDigit
		_ func(byte) bool                            = isJSONWhitespace
	)

	extractorSource := readAdapterSource(t, "claude_cwd_json_extractor.go")
	helperSource := readAdapterSource(t, "claude_cwd_json_extractor_helpers.go")
	if !strings.Contains(extractorSource, "func (e *cwdMetadataExtractor) feed(") {
		t.Error("feed is not defined in claude_cwd_json_extractor.go")
	}
	for _, name := range []string{
		"startKeyString",
		"startValue",
		"feedStringByte",
		"closeContainer",
		"finishValue",
		"feedNumberByte",
		"numberCanEnd",
		"matchKeyByte",
	} {
		if strings.Contains(extractorSource, "func (e *cwdMetadataExtractor) "+name+"(") {
			t.Errorf("%s is still defined in claude_cwd_json_extractor.go", name)
		}
		if !strings.Contains(helperSource, "func (e *cwdMetadataExtractor) "+name+"(") {
			t.Errorf("%s is not defined in claude_cwd_json_extractor_helpers.go", name)
		}
	}
	for _, name := range []string{
		"isJSONValueDelimiter",
		"isJSONHexDigit",
		"isJSONWhitespace",
	} {
		if strings.Contains(extractorSource, "func "+name+"(") {
			t.Errorf("%s is still defined in claude_cwd_json_extractor.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in claude_cwd_json_extractor_helpers.go", name)
		}
	}
	for _, call := range []string{
		"isJSONWhitespace(",
		"e.feedStringByte(",
		"e.feedNumberByte(",
		"e.numberCanEnd(",
		"isJSONValueDelimiter(",
		"e.startKeyString(",
		"e.closeContainer(",
		"e.startValue(",
	} {
		if !strings.Contains(extractorSource, call) {
			t.Errorf("claude_cwd_json_extractor.go no longer delegates to %s", call)
		}
	}
}
