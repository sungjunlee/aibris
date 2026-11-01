package safedelete

import (
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestNoRemoveAllOutsideTheGate keeps this package the only place that can
// recursively delete. Outside it, production code may not reference
// os.RemoveAll or call (*os.Root).RemoveAll, including a method value.
// os.OpenRoot is allowed only where strip opens a unit, which removes
// through RemoveAllIn.
func TestNoRemoveAllOutsideTheGate(t *testing.T) {
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "go.mod")); err != nil {
		t.Fatalf("repository root not found: %v", err)
	}
	hits, err := deletionGateViolations(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		t.Error(hit)
	}

	t.Run("fixtures", func(t *testing.T) {
		testDeletionGateFixtures(t)
	})
}

func testDeletionGateFixtures(t *testing.T) {
	t.Helper()
	negative := []struct {
		name   string
		files  map[string]string
		want   string
		absent string
	}{
		{
			name: "os.RemoveAll",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport \"os\"\nfunc unused() { os.RemoveAll(\"victim\") }\n",
			},
			want: "os.RemoveAll",
		},
		{
			name: "import alias",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport oss \"os\"\nfunc unused() { oss.RemoveAll(\"victim\") }\n",
			},
			want: "os.RemoveAll",
		},
		{
			name: "dot import",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport . \"os\"\nfunc unused() { RemoveAll(\"victim\") }\n",
			},
			want: "dot-imported",
		},
		{
			name: "OpenRoot outside strip",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport \"os\"\nfunc unused() { _, _ = os.OpenRoot(\"unit\") }\n",
			},
			want: "os.OpenRoot",
		},
		{
			name: "root.RemoveAll on the strip path",
			files: map[string]string{
				"internal/worktree/strip.go": "package worktree\nimport \"os\"\nfunc unused() {\n\troot, err := os.OpenRoot(\"unit\")\n\tif err != nil { return }\n\troot.RemoveAll(\"victim\")\n}\n",
			},
			want:   "*os.Root.RemoveAll",
			absent: "os.OpenRoot",
		},
		{
			name: "method value",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport \"os\"\nfunc unused() {\n\troot, err := os.OpenRoot(\"unit\")\n\tif err != nil { return }\n\tfn := root.RemoveAll\n\t_ = fn\n}\n",
			},
			want: "*os.Root.RemoveAll",
		},
		{
			name: "method expression",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport \"os\"\nfunc unused() { _ = (*os.Root).RemoveAll }\n",
			},
			want: "*os.Root.RemoveAll",
		},
		{
			name: "aliased method expression",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport oss \"os\"\nfunc unused() { _ = (*oss.Root).RemoveAll }\n",
			},
			want: "*os.Root.RemoveAll",
		},
		{
			name: "addressable value",
			files: map[string]string{
				"internal/other/bad.go": "package other\nimport \"os\"\nfunc unused() { var root os.Root; root.RemoveAll(\"victim\") }\n",
			},
			want: "*os.Root.RemoveAll",
		},
		{
			name: "windows file",
			files: map[string]string{
				"internal/other/bad_windows.go": "//go:build windows\n\npackage other\n\nimport \"os\"\n\nfunc unused(root *os.Root) { root.RemoveAll(\"victim\") }\n",
			},
			want: "*os.Root.RemoveAll",
		},
	}
	for _, tt := range negative {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := deletionGateViolations(writeFixture(t, tt.files))
			if err != nil {
				t.Fatal(err)
			}
			if !containsHit(hits, tt.want) {
				t.Fatalf("violations = %v; want %q", hits, tt.want)
			}
			if tt.absent != "" && containsHit(hits, tt.absent) {
				t.Fatalf("violations = %v; %q must stay allowed", hits, tt.absent)
			}
		})
	}

	positive := []struct {
		name  string
		files map[string]string
	}{
		{
			name: "gate package",
			files: map[string]string{
				"internal/safedelete/gate.go": "package safedelete\nimport \"os\"\nfunc remove(root *os.Root, path, rel string) error {\n\tif err := os.RemoveAll(path); err != nil { return err }\n\t_, _ = os.OpenRoot(path)\n\treturn root.RemoveAll(rel)\n}\n",
			},
		},
		{
			name: "strip delegates removal",
			files: map[string]string{
				"internal/worktree/strip.go": "package worktree\nimport \"os\"\nfunc open(path, rel string) {\n\troot, err := os.OpenRoot(path)\n\tif err != nil { return }\n\tdefer root.Close()\n\tremoveAllIn(root, rel)\n}\nfunc removeAllIn(root *os.Root, rel string) {}\n",
			},
		},
		{
			name: "func field",
			files: map[string]string{
				"internal/other/ok.go": "package other\ntype opts struct { RemoveAll func(string) error }\nfunc unused(o opts) { o.RemoveAll(\"path\") }\n",
			},
		},
	}
	for _, tt := range positive {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := deletionGateViolations(writeFixture(t, tt.files))
			if err != nil {
				t.Fatal(err)
			}
			if len(hits) != 0 {
				t.Fatalf("legal deletion path flagged: %v", hits)
			}
		})
	}
}

func writeFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func containsHit(hits []string, needle string) bool {
	for _, hit := range hits {
		if strings.Contains(hit, needle) {
			return true
		}
	}
	return false
}

func deletionGateViolations(repo string) ([]string, error) {
	hits, err := astDeletionViolations(repo)
	if err != nil {
		return nil, err
	}
	typeHits, err := rootRemoveAllViolations(repo)
	if err != nil {
		return nil, err
	}
	hits = append(hits, typeHits...)
	slices.Sort(hits)
	return slices.Compact(hits), nil
}

func astDeletionViolations(repo string) ([]string, error) {
	gate := filepath.Join(repo, "internal", "safedelete")
	openRootAllowed := filepath.Join(repo, "internal", "worktree", "strip.go")
	var hits []string
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path == gate || path == filepath.Join(repo, "tools") ||
				(path != repo && strings.HasPrefix(name, ".")) || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		osNames := map[string]bool{}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil || importPath != "os" {
				continue
			}
			name := "os"
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name == "." {
				hits = append(hits, rel+": package os dot-imported; use a named import")
			}
			osNames[name] = true
		}
		if len(osNames) == 0 {
			return nil
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			recv, ok := sel.X.(*ast.Ident)
			if !ok || !osNames[recv.Name] {
				return true
			}
			switch {
			case sel.Sel.Name == "RemoveAll":
				hits = append(hits, rel+": os.RemoveAll outside internal/safedelete")
			case sel.Sel.Name == "OpenRoot" && path != openRootAllowed:
				hits = append(hits, rel+": os.OpenRoot outside internal/safedelete and strip")
			}
			return true
		})
		return nil
	})
	return hits, err
}

func rootRemoveAllViolations(repo string) ([]string, error) {
	imp := newSelectiveImporter()
	var hits []string
	checked := map[string]bool{}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		ctx := build.Default
		ctx.GOOS = goos
		ctx.GOARCH = "amd64"
		found, err := rootRemoveAllInContext(repo, ctx, imp, checked)
		if err != nil {
			return nil, err
		}
		hits = append(hits, found...)
	}
	leftovers, err := uncheckedRootRemoveAll(repo, imp, checked)
	if err != nil {
		return nil, err
	}
	return append(hits, leftovers...), nil
}

func rootRemoveAllInContext(repo string, ctx build.Context, imp types.Importer, checked map[string]bool) ([]string, error) {
	gate := filepath.Join(repo, "internal", "safedelete")
	var hits []string
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path == gate || path == filepath.Join(repo, "tools") ||
			(path != repo && strings.HasPrefix(name, ".")) || name == "testdata" {
			return filepath.SkipDir
		}
		files, err := productionGoFiles(ctx, path)
		if err != nil {
			return err
		}
		if !filesMentionRemoveAll(path, files) {
			return nil
		}
		found, err := typeCheckRootRemoveAll(repo, path, files, imp)
		if err != nil {
			return err
		}
		hits = append(hits, found...)
		for _, name := range files {
			checked[filepath.Join(path, name)] = true
		}
		return nil
	})
	return hits, err
}

// uncheckedRootRemoveAll type-checks production files that no target GOOS
// selects, such as a build-ignored file. A bypass there is still a bypass.
func uncheckedRootRemoveAll(repo string, imp types.Importer, checked map[string]bool) ([]string, error) {
	gate := filepath.Join(repo, "internal", "safedelete")
	var hits []string
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path == gate || path == filepath.Join(repo, "tools") ||
				(path != repo && strings.HasPrefix(name, ".")) || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || checked[path] {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), "RemoveAll") {
			return nil
		}
		found, err := typeCheckRootRemoveAll(repo, filepath.Dir(path), []string{filepath.Base(path)}, imp)
		if err != nil {
			return err
		}
		hits = append(hits, found...)
		return nil
	})
	return hits, err
}

func productionGoFiles(ctx build.Context, dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		ok, err := ctx.MatchFile(dir, name)
		if err != nil {
			return nil, err
		}
		if ok {
			files = append(files, name)
		}
	}
	return files, nil
}

func filesMentionRemoveAll(dir string, files []string) bool {
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err == nil && strings.Contains(string(body), "RemoveAll") {
			return true
		}
	}
	return false
}

func typeCheckRootRemoveAll(repo, dir string, files []string, imp types.Importer) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	for _, name := range files {
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, file)
	}
	info := &types.Info{Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	_, _ = conf.Check(parsed[0].Name.Name, fset, parsed, info)
	var hits []string
	for _, file := range parsed {
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok || !isOSRootRemoveAll(info, sel) {
				return true
			}
			pos := fset.Position(sel.Pos())
			rel, err := filepath.Rel(repo, pos.Filename)
			if err != nil {
				rel = pos.Filename
			}
			hits = append(hits, rel+": *os.Root.RemoveAll outside internal/safedelete")
			return true
		})
	}
	return hits, nil
}

func isOSRootRemoveAll(info *types.Info, sel *ast.SelectorExpr) bool {
	if sel.Sel.Name != "RemoveAll" {
		return false
	}
	selection, ok := info.Selections[sel]
	if !ok {
		return false
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	return isPointerToOSRoot(sig.Recv().Type())
}

func isPointerToOSRoot(typ types.Type) bool {
	typ = types.Unalias(typ)
	ptr, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := types.Unalias(ptr.Elem()).(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Root" && named.Obj().Pkg().Path() == "os"
}

type selectiveImporter struct {
	base types.Importer
	fake map[string]*types.Package
}

func newSelectiveImporter() *selectiveImporter {
	return &selectiveImporter{base: importer.Default(), fake: map[string]*types.Package{}}
}

func (s *selectiveImporter) Import(path string) (*types.Package, error) {
	return s.ImportFrom(path, "", 0)
}

func (s *selectiveImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if isStdlibImport(path) {
		if from, ok := s.base.(types.ImporterFrom); ok {
			return from.ImportFrom(path, dir, mode)
		}
		return s.base.Import(path)
	}
	if pkg, ok := s.fake[path]; ok {
		return pkg, nil
	}
	pkg := types.NewPackage(path, path)
	pkg.MarkComplete()
	s.fake[path] = pkg
	return pkg, nil
}

func isStdlibImport(path string) bool {
	head, _, _ := strings.Cut(path, "/")
	return !strings.Contains(head, ".")
}
