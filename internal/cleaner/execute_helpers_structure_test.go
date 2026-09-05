package cleaner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteMutationHelpersLiveApartFromExecuteEntry(t *testing.T) {
	helperNames := []string{
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
		"executeWithContext",
		"executeWithContextOutput",
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
	var (
		_ func([]types.DebrisInfo) (int64, error) = Execute
		_                                         = ExecuteWithContext
		_                                         = ExecuteWithContextAndBarrier
		_                                         = ExecuteWithContextAndBarrierWithOutput
		_                                         = ExecuteWithContextAndBarrierWithOutputAndObserver
	)
}
