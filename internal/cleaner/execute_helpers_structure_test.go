package cleaner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cleaner after the same-package extract.
var (
	_ = executeWithContext
	_ = executeWithContextOutput
	_ = Execute
	_ = ExecuteWithContext
	_ = ExecuteWithContextAndBarrier
	_ = ExecuteWithContextAndBarrierWithOutput
	_ = ExecuteWithContextAndBarrierWithOutputAndObserver
)

func TestExecuteMutationHelpersLiveApartFromExecuteEntry(t *testing.T) {
	helperNames := []string{
		"executeWithContext",
		"executeWithContextOutput",
		"runMutationBarrier",
		"debrisName",
		"cleanupKind",
		"refuseStaleGoCache",
		"isGoCleanCache",
		"reportCommandCleaned",
		"reportCommandResidual",
		"runCleanupCommand",
	}
	executeNames := []string{
		"Execute",
		"ExecuteWithContext",
		"ExecuteWithContextAndBarrier",
		"ExecuteWithContextAndBarrierWithOutput",
		"ExecuteWithContextAndBarrierWithOutputAndObserver",
	}

	wanted := make(map[string]string, len(helperNames)+len(executeNames))
	for _, name := range helperNames {
		wanted[name] = "execute_helpers.go"
	}
	for _, name := range executeNames {
		wanted[name] = "execute.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cleaner: %v", err)
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

func TestExecuteEntryReexportIdentity(t *testing.T) {
	helpers := []any{
		executeWithContext,
		executeWithContextOutput,
	}
	public := []any{
		Execute,
		ExecuteWithContext,
		ExecuteWithContextAndBarrier,
		ExecuteWithContextAndBarrierWithOutput,
		ExecuteWithContextAndBarrierWithOutputAndObserver,
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
		_ func([]types.DebrisInfo) (int64, error) = Execute
		_                                         = ExecuteWithContext
		_                                         = ExecuteWithContextAndBarrier
		_                                         = ExecuteWithContextAndBarrierWithOutput
		_                                         = ExecuteWithContextAndBarrierWithOutputAndObserver
	)

	executeSource := readCleanerSource(t, "execute.go")
	helperSource := readCleanerSource(t, "execute_helpers.go")
	for _, name := range []string{
		"Execute",
		"ExecuteWithContext",
		"ExecuteWithContextAndBarrier",
		"ExecuteWithContextAndBarrierWithOutput",
		"ExecuteWithContextAndBarrierWithOutputAndObserver",
	} {
		if !strings.Contains(executeSource, "func "+name+"(") {
			t.Errorf("%s is not defined in execute.go", name)
		}
		if strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s moved out of the public execute entry", name)
		}
	}
	for _, name := range []string{
		"executeWithContext",
		"executeWithContextOutput",
	} {
		if strings.Contains(executeSource, "func "+name+"(") {
			t.Errorf("%s is still defined in execute.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in execute_helpers.go", name)
		}
	}
}
