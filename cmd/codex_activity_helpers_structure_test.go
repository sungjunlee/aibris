package cmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodexActivityHelpersLiveApartFromIndexLoader(t *testing.T) {
	helperNames := []string{
		"findCodexSessionFiles",
		"appendSessionFileInfo",
		"readCodexSessionFileRecord",
		"codexActivityWorktreeFromCWD",
		"pathParts",
		"isCodexActivityWorktreeRoot",
		"firstNonEmpty",
	}
	loaderNames := []string{
		"loadCodexActivityIndex",
		"loadCodexActivityIndexWithOptions",
	}

	wanted := make(map[string]string, len(helperNames)+len(loaderNames))
	for _, name := range helperNames {
		wanted[name] = "codex_activity_helpers.go"
	}
	for _, name := range loaderNames {
		wanted[name] = "codex_activity.go"
	}

	owners := functionOwners(t, wanted)
	for name, owner := range wanted {
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestCodexActivityHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	helpers := map[string]any{
		"findCodexSessionFiles":             findCodexSessionFiles,
		"appendSessionFileInfo":             appendSessionFileInfo,
		"readCodexSessionFileRecord":        readCodexSessionFileRecord,
		"codexActivityWorktreeFromCWD":      codexActivityWorktreeFromCWD,
		"pathParts":                         pathParts,
		"isCodexActivityWorktreeRoot":       isCodexActivityWorktreeRoot,
		"firstNonEmpty":                     firstNonEmpty,
		"loadCodexActivityIndex":            loadCodexActivityIndex,
		"loadCodexActivityIndexWithOptions": loadCodexActivityIndexWithOptions,
	}
	for name, fn := range helpers {
		value := reflect.ValueOf(fn)
		if !value.IsValid() || value.Kind() != reflect.Func || value.IsNil() {
			t.Errorf("%s is not a live package function; want original names to keep resolving", name)
		}
	}

	original := readCmdSource(t, "codex_activity.go")
	if !strings.Contains(original, "func loadCodexActivityIndex(") {
		t.Error("loadCodexActivityIndex is not defined in codex_activity.go")
	}
	if !strings.Contains(original, "func loadCodexActivityIndexWithOptions(") {
		t.Error("loadCodexActivityIndexWithOptions is not defined in codex_activity.go")
	}
	for _, name := range []string{"findCodexSessionFiles", "readCodexSessionFileRecord"} {
		if strings.Contains(original, "func "+name+"(") {
			t.Errorf("%s is still defined in codex_activity.go", name)
		}
		if !strings.Contains(original, name+"(") {
			t.Errorf("codex_activity.go no longer delegates to %s", name)
		}
	}
}

func functionOwners(t *testing.T, wanted map[string]string) map[string][]string {
	t.Helper()
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
	return owners
}
