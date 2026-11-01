package test

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestVerificationModuleContract(t *testing.T) {
	ci := readWorkflowContract(t, ".github/workflows/ci.yml")
	check := false
	for _, step := range ci.Jobs["check"].Steps {
		if step.Run == "make check" {
			check = true
		}
	}
	if !check {
		t.Error("shared CI must run the default development check")
	}
	if ci.Jobs["windows-safety"].RunsOn != "windows-latest" || ci.Jobs["test"].RunsOn != "${{ matrix.os }}" {
		t.Error("platform verification must use native Windows and the Linux/macOS matrix")
	}
	if !slices.Contains(ci.Jobs["test"].Strategy.Matrix["os"], "ubuntu-latest") || !slices.Contains(ci.Jobs["test"].Strategy.Matrix["os"], "macos-latest") {
		t.Error("module verification must run on Linux and macOS")
	}
	var runs []string
	for _, step := range ci.Jobs["test"].Steps {
		runs = append(runs, step.Run)
	}
	for _, command := range []string{"go test -race -count=1 -cover ./...", "go -C tools/perfharness test -race -count=1 -cover ./...", "make lint"} {
		if !slices.Contains(runs, command) {
			t.Errorf("Linux/macOS missing module verification: %s", command)
		}
	}
	windows := false
	for _, step := range ci.Jobs["windows-safety"].Steps {
		if strings.Contains(step.Run, "go test -race -count=1 -cover ./internal/safedelete ./internal/pathidentity ./internal/testutil") && !strings.Contains(step.Run, "-run") {
			windows = true
		}
	}
	if !windows {
		t.Error("Windows must run whole deletion-gate, identity, and hermeticity packages")
	}
	verifyWindowsDedicatedTests(t, ci)
	if runtime.GOOS == "windows" {
		t.Skip("Makefile is a Unix developer entry point; native Windows uses explicit package commands")
	}
	for _, target := range []string{"test", "test-race", "lint", "check"} {
		out, err := exec.Command("make", "-n", target).CombinedOutput()
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", target, err, out)
		}
		commands := string(out)
		if target == "lint" || target == "check" {
			for _, command := range []string{"go -C tools/perfharness vet ./...", "go -C tools/perfharness run honnef.co/go/tools/cmd/staticcheck@"} {
				if !strings.Contains(commands, command) {
					t.Errorf("make %s missing %s", target, command)
				}
			}
		} else {
			if !strings.Contains(commands, "go -C tools/perfharness test ") || !strings.Contains(commands, "go test ") {
				t.Errorf("make %s must test both modules", target)
			}
		}
	}
}

// verifyWindowsDedicatedTests fails when a windows-only test is absent from
// the Windows job, or when a -run list matches nothing. Whole packages are
// covered without a name list. Selected runs go through citestjson, which
// rejects an empty result and counts skips apart from passes.
func verifyWindowsDedicatedTests(t *testing.T, ci workflowContract) {
	t.Helper()
	if err := errIfEmptySelection(0, "example", "^missing$"); err == nil {
		t.Error("zero test selection must not be success")
	}
	if err := errIfEmptySelection(1, "example", "^Test$"); err != nil {
		t.Error(err)
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, ".github", "scripts", "go-test-json.ps1")
	scriptText, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scriptText), "go test -json") || !strings.Contains(string(scriptText), "citestjson.exe") {
		t.Error("selected Windows tests must run go test -json through citestjson")
	}

	steps := ci.Jobs["windows-safety"].Steps
	buildAt, firstUse := -1, -1
	var invocations []windowsTestInvocation
	for i, step := range steps {
		if strings.Contains(step.Run, "go build -o citestjson.exe ./tools/citestjson") {
			buildAt = i
		}
		if strings.Contains(step.Run, "go-test-json.ps1") && firstUse < 0 {
			firstUse = i
		}
		if strings.Contains(step.Run, "-run ") && !strings.Contains(step.Run, "go-test-json.ps1") {
			t.Errorf("selected Windows test must run through go-test-json.ps1 so an empty match is not success:\n%s", step.Run)
		}
		found, err := parseWindowsTestInvocations(step.Run)
		if err != nil {
			t.Fatal(err)
		}
		invocations = append(invocations, found...)
	}
	if buildAt < 0 || firstUse < 0 || buildAt > firstUse {
		t.Error("Windows job must build ./tools/citestjson before a selected test")
	}

	win := testsForGOOS(t, root, "windows")
	linux := testsForGOOS(t, root, "linux")
	darwin := testsForGOOS(t, root, "darwin")
	for dir, names := range map[string][]string{
		"internal/adapter": {
			"TestWorktreeAdapter_WindowsMixedCaseRegisteredIdentity",
			"TestWorktreeAdapter_WindowsBlockedAliasIsNotReintroduced",
		},
		"internal/cleanjson": {"TestWindowsPathRedaction"},
	} {
		for _, name := range names {
			if !win[dir][name] || linux[dir][name] || darwin[dir][name] {
				t.Errorf("%s.%s must be discovered as windows-only", dir, name)
			}
		}
	}
	for _, inv := range invocations {
		if inv.run == "" {
			continue
		}
		re, err := regexp.Compile(inv.run)
		if err != nil {
			t.Errorf("invalid -run %q: %v", inv.run, err)
			continue
		}
		for _, pkg := range inv.packages {
			if pkg == "tools/perfharness/..." {
				continue
			}
			n := 0
			for name := range win[pkg] {
				if re.MatchString(name) {
					n++
				}
			}
			if err := errIfEmptySelection(n, pkg, inv.run); err != nil {
				t.Error(err)
			}
		}
	}

	var missing []string
	for dir, tests := range win {
		for name := range tests {
			if linux[dir][name] || darwin[dir][name] {
				continue
			}
			ok, err := windowsInvocationCovers(invocations, dir, name)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				missing = append(missing, dir+"."+name)
			}
		}
	}
	slices.Sort(missing)
	for _, name := range missing {
		t.Errorf("windows-only test is not selected by Windows CI: %s", name)
	}
}

func errIfEmptySelection(matched int, pkg, pattern string) error {
	if matched == 0 {
		return fmt.Errorf("Windows selection matched no tests in %s: -run %s", pkg, pattern)
	}
	return nil
}

type windowsTestInvocation struct {
	packages []string
	run      string
}

var (
	windowsRunPattern = regexp.MustCompile(`-run\s+'([^']*)'`)
	windowsPkgPattern = regexp.MustCompile(`(?:^|\s)(\./[A-Za-z0-9_./-]+)`)
	perfHarnessTest   = regexp.MustCompile(`go\s+-C\s+tools/perfharness\s+test\b`)
)

func parseWindowsTestInvocations(run string) ([]windowsTestInvocation, error) {
	var out []windowsTestInvocation
	for _, line := range strings.Split(run, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		inv, ok, err := parseWindowsTestLine(line)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, inv)
		}
	}
	return out, nil
}

func parseWindowsTestLine(line string) (windowsTestInvocation, bool, error) {
	if perfHarnessTest.MatchString(line) {
		inv := windowsTestInvocation{packages: []string{"tools/perfharness/..."}}
		if m := windowsRunPattern.FindStringSubmatch(line); m != nil {
			inv.packages = []string{"tools/perfharness"}
			inv.run = m[1]
		}
		return inv, true, nil
	}
	if !strings.Contains(line, "go test") && !strings.Contains(line, "go-test-json.ps1") {
		return windowsTestInvocation{}, false, nil
	}
	matches := windowsPkgPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return windowsTestInvocation{}, false, fmt.Errorf("windows test command has no package: %s", line)
	}
	inv := windowsTestInvocation{}
	for _, match := range matches {
		inv.packages = append(inv.packages, strings.TrimPrefix(match[1], "./"))
	}
	if m := windowsRunPattern.FindStringSubmatch(line); m != nil {
		inv.run = m[1]
	}
	return inv, true, nil
}

func windowsInvocationCovers(invocations []windowsTestInvocation, dir, name string) (bool, error) {
	for _, inv := range invocations {
		for _, pkg := range inv.packages {
			if pkg == "tools/perfharness/..." {
				if inv.run == "" && (dir == "tools/perfharness" || strings.HasPrefix(dir, "tools/perfharness/")) {
					return true, nil
				}
				continue
			}
			if pkg != dir {
				continue
			}
			if inv.run == "" {
				return true, nil
			}
			re, err := regexp.Compile(inv.run)
			if err != nil {
				return false, err
			}
			if re.MatchString(name) {
				return true, nil
			}
		}
	}
	return false, nil
}

func testsForGOOS(t *testing.T, root, goos string) map[string]map[string]bool {
	t.Helper()
	ctx := build.Default
	ctx.GOOS = goos
	ctx.GOARCH = "amd64"
	out := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
				continue
			}
			ok, err := ctx.MatchFile(path, name)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			funcs, err := testFuncsInFile(filepath.Join(path, name))
			if err != nil {
				return err
			}
			if len(funcs) == 0 {
				continue
			}
			set := out[rel]
			if set == nil {
				set = map[string]bool{}
				out[rel] = set
			}
			for _, fn := range funcs {
				set[fn] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func testFuncsInFile(path string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && isGoTestFunc(fn) {
			names = append(names, fn.Name.Name)
		}
	}
	return names, nil
}

func isGoTestFunc(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Recv != nil || fn.Name == nil || fn.Body == nil {
		return false
	}
	name := fn.Name.Name
	if name == "TestMain" || !strings.HasPrefix(name, "Test") || !ast.IsExported(name) {
		return false
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) != 1 {
		return false
	}
	if fn.Type.Results != nil && len(fn.Type.Results.List) != 0 {
		return false
	}
	star, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "testing"
}
