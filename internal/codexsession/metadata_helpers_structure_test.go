package codexsession

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Compile-time re-export identity: the original helper names still resolve
// in package codexsession after the same-package extract.
var (
	_ = decodeMetadata
	_ = decodePayload
	_ = objectKey
	_ = scalarString
	_ = duplicate
	_ = discardValue
	_ = hasUnpairedSurrogateEscape
	_ = hexQuad
	_ = ReadFirstMetadata
	_ = ReadFirstMetadataFrom
	_ = ErrorKindOf
	_ = IsEvidenceError
)

func TestMetadataHelpersLiveApartFromPublicReadEntry(t *testing.T) {
	helperNames := []string{
		"decodeMetadata",
		"decodePayload",
		"objectKey",
		"scalarString",
		"duplicate",
		"discardValue",
		"hasUnpairedSurrogateEscape",
		"hexQuad",
	}
	facadeNames := []string{
		"ReadFirstMetadata",
		"ReadFirstMetadataFrom",
		"ErrorKindOf",
		"IsEvidenceError",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "metadata_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "metadata.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse codexsession: %v", err)
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

func TestMetadataHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing codexsession callers still resolve to the helper implementations.
	helpers := []any{
		decodeMetadata,
		decodePayload,
		objectKey,
		scalarString,
		duplicate,
		discardValue,
		hasUnpairedSurrogateEscape,
		hexQuad,
	}
	public := []any{
		ReadFirstMetadata,
		ReadFirstMetadataFrom,
		ErrorKindOf,
		IsEvidenceError,
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
		_ func([]byte) (Metadata, error)                     = decodeMetadata
		_ func(*json.Decoder, *Metadata) error               = decodePayload
		_ func(*json.Decoder) (string, error)                = objectKey
		_ func(*json.Decoder) (string, error)                = scalarString
		_ func(map[string]bool, string) bool                 = duplicate
		_ func(*json.Decoder, int) error                     = discardValue
		_ func([]byte) bool                                  = hasUnpairedSurrogateEscape
		_ func([]byte, int) (uint16, bool)                   = hexQuad
		_ func(context.Context, string) (Metadata, error)    = ReadFirstMetadata
		_ func(context.Context, io.Reader) (Metadata, error) = ReadFirstMetadataFrom
		_ func(error) (ErrorKind, bool)                      = ErrorKindOf
		_ func(error) bool                                   = IsEvidenceError
	)

	metadataSource := readCodexsessionSource(t, "metadata.go")
	if !strings.Contains(metadataSource, "func ReadFirstMetadata(") {
		t.Error("ReadFirstMetadata is not defined in metadata.go")
	}
	if !strings.Contains(metadataSource, "func ReadFirstMetadataFrom(") {
		t.Error("ReadFirstMetadataFrom is not defined in metadata.go")
	}
	if !strings.Contains(metadataSource, "decodeMetadata(") {
		t.Error("metadata.go no longer delegates to decodeMetadata")
	}
	for _, name := range []string{
		"decodeMetadata",
		"decodePayload",
		"objectKey",
		"scalarString",
		"duplicate",
		"discardValue",
		"hasUnpairedSurrogateEscape",
		"hexQuad",
	} {
		if strings.Contains(metadataSource, "func "+name+"(") {
			t.Errorf("%s is still defined in metadata.go", name)
		}
	}
}

func readCodexsessionSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
