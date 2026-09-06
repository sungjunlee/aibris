package cleanjson

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
)

// Compile-time re-export identity: the original helper names still resolve
// in package cleanjson after the same-package extract.
var (
	_ = refusePartialScan
	_ = evidenceFor
	_ = cleanupKind
	_ = Build
	_ = Render
	_ = Encode
)

func TestDocumentHelpersLiveApartFromPublicDocumentEntry(t *testing.T) {
	helperNames := []string{
		"refusePartialScan",
		"evidenceFor",
		"cleanupKind",
	}
	entryNames := []string{
		"Build",
		"Render",
		"Encode",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "document_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "document.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cleanjson: %v", err)
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

func TestDocumentHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cleanjson callers still resolve to the helper implementations.
	var (
		_ func(*types.ScanResult) error            = refusePartialScan
		_ func(Source, PlanEvidence) Evidence      = evidenceFor
		_ func(types.DebrisInfo) types.CleanupKind = cleanupKind
		_ func(Input) (Plan, error)                = Build
		_ func(Input, []SnapshotComponent) Plan    = Render
		_ func(io.Writer, Plan) error              = Encode
	)

	documentSource := readCleanjsonSource(t, "document.go")
	if !strings.Contains(documentSource, "func Build(") {
		t.Error("Build is not defined in document.go")
	}
	if !strings.Contains(documentSource, "func Render(") {
		t.Error("Render is not defined in document.go")
	}
	if !strings.Contains(documentSource, "func Encode(") {
		t.Error("Encode is not defined in document.go")
	}
	if !strings.Contains(documentSource, "refusePartialScan(") {
		t.Error("document.go no longer delegates to refusePartialScan")
	}
	if !strings.Contains(documentSource, "evidenceFor(") {
		t.Error("document.go no longer delegates to evidenceFor")
	}
	for _, name := range []string{
		"refusePartialScan",
		"evidenceFor",
		"cleanupKind",
	} {
		if strings.Contains(documentSource, "func "+name+"(") {
			t.Errorf("%s is still defined in document.go", name)
		}
	}
}

func readCleanjsonSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
