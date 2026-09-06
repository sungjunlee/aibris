package retention

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/codexsession"
	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package retention after the same-package extract.
var (
	_                         = codexSessionsRoot
	_                         = classifiableMetadata
	_                         = rootsCoveringCodexHome
	_                         = storeSelected
	_                         = validYear
	_                         = validMonth
	_                         = validDay
	_                         = isRolloutName
	_                         = bucketFromModTime
	_                         = usableRecordedCWD
	_                         = NewCodexSessionsProvider
	_ types.RetentionProvider = (*CodexSessionsProvider)(nil)
)

func TestCodexSessionsHelpersLiveApartFromListEntry(t *testing.T) {
	helperNames := []string{
		"codexSessionsRoot",
		"classifiableMetadata",
		"rootsCoveringCodexHome",
		"storeSelected",
		"validYear",
		"validMonth",
		"validDay",
		"isRolloutName",
		"bucketFromModTime",
		"usableRecordedCWD",
	}
	facadeNames := []string{
		"NewCodexSessionsProvider",
		"Name",
		"Scan",
		"newInventoryState",
		"emptyProjection",
		"addProviderError",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "codex_sessions_helpers.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "codex_sessions.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse retention: %v", err)
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

func TestCodexSessionsHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing retention callers still resolve to the helper implementations.
	helpers := []any{
		codexSessionsRoot,
		classifiableMetadata,
		rootsCoveringCodexHome,
		storeSelected,
		validYear,
		validMonth,
		validDay,
		isRolloutName,
		bucketFromModTime,
		usableRecordedCWD,
	}
	public := []any{
		NewCodexSessionsProvider,
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
		_ func() *CodexSessionsProvider     = NewCodexSessionsProvider
		_ func() (string, error)            = codexSessionsRoot
		_ func(codexsession.Metadata) bool  = classifiableMetadata
		_ func([]string) []string           = rootsCoveringCodexHome
		_ func(string, []string) bool       = storeSelected
		_ func(string) bool                 = validYear
		_ func(string) bool                 = validMonth
		_ func(string, string, string) bool = validDay
		_ func(string) bool                 = isRolloutName
		_ func(time.Time) string            = bucketFromModTime
		_ func(string) bool                 = usableRecordedCWD
		_ types.RetentionProvider           = (*CodexSessionsProvider)(nil)
	)

	sessionsSource := readRetentionSource(t, "codex_sessions.go")
	if !strings.Contains(sessionsSource, "func (p *CodexSessionsProvider) Scan(") {
		t.Error("Scan is not defined in codex_sessions.go")
	}
	if !strings.Contains(sessionsSource, "func NewCodexSessionsProvider(") {
		t.Error("NewCodexSessionsProvider is not defined in codex_sessions.go")
	}
	for _, name := range []string{
		"codexSessionsRoot",
		"classifiableMetadata",
		"rootsCoveringCodexHome",
		"storeSelected",
		"validYear",
		"validMonth",
		"validDay",
		"isRolloutName",
		"bucketFromModTime",
		"usableRecordedCWD",
	} {
		if strings.Contains(sessionsSource, "func "+name+"(") {
			t.Errorf("%s is still defined in codex_sessions.go", name)
		}
	}
	if !strings.Contains(sessionsSource, "codexSessionsRoot()") {
		t.Error("codex_sessions.go no longer delegates to codexSessionsRoot")
	}
	if !strings.Contains(sessionsSource, "classifiableMetadata(") {
		t.Error("codex_sessions.go no longer delegates to classifiableMetadata")
	}
	if !strings.Contains(sessionsSource, "rootsCoveringCodexHome(") {
		t.Error("codex_sessions.go no longer delegates to rootsCoveringCodexHome")
	}
	if !strings.Contains(sessionsSource, "storeSelected(") {
		t.Error("codex_sessions.go no longer delegates to storeSelected")
	}
}

func readRetentionSource(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
