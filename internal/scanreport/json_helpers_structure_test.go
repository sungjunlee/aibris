package scanreport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

// Compile-time re-export identity: the original helper names still resolve
// in package scanreport after the same-package extract.
var (
	_ = WriteJSON
	_ = EncodeJSON
	_ = applyPhysicalJSONSummary
	_ = JSONVolumeFromReport
)

func TestJSONHelpersLiveApartFromPublicJSONEntry(t *testing.T) {
	helperNames := []string{
		"EncodeJSON",
		"applyPhysicalJSONSummary",
		"JSONVolumeFromReport",
	}
	facadeNames := []string{
		"WriteJSON",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "json_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "json.go"
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

func TestJSONHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing scanreport callers still resolve to the helper implementations.
	helpers := []any{
		EncodeJSON,
		applyPhysicalJSONSummary,
		JSONVolumeFromReport,
	}
	public := []any{
		WriteJSON,
		EncodeJSON,
		JSONVolumeFromReport,
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
		_ func(io.Writer, View)                 = WriteJSON
		_ func(View) JSONOutput                 = EncodeJSON
		_ func(*JSONOutput, []types.DebrisInfo) = applyPhysicalJSONSummary
		_ func(volume.Report) *JSONVolume       = JSONVolumeFromReport
	)

	jsonSource := readScanreportSource(t, "json.go")
	helperSource := readScanreportSource(t, "json_helpers.go")
	if !strings.Contains(jsonSource, "func WriteJSON(") {
		t.Error("WriteJSON is not defined in json.go")
	}
	if strings.Contains(helperSource, "func WriteJSON(") {
		t.Error("WriteJSON moved out of the public JSON entry")
	}
	for _, name := range []string{
		"EncodeJSON",
		"applyPhysicalJSONSummary",
		"JSONVolumeFromReport",
	} {
		if strings.Contains(jsonSource, "func "+name+"(") {
			t.Errorf("%s is still defined in json.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in json_helpers.go", name)
		}
	}
	if !strings.Contains(jsonSource, "EncodeJSON(") {
		t.Error("json.go no longer delegates to EncodeJSON")
	}
}
