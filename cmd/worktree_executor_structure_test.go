package cmd

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Compile-time re-export identity: the original execute path names still
// resolve in package cmd after the same-package extract.
var (
	_ = executeCleanTargets
	_ = executePreparedCleanTargets
	_ = defaultActiveWorktreeExecutionOptions
)

func TestWorktreeExecutorExecutePathsLiveApart(t *testing.T) {
	unpreparedNames := []string{
		"executeCleanTargets",
		"defaultActiveWorktreeExecutionOptions",
	}
	preparedNames := []string{
		"executePreparedCleanTargets",
	}

	wanted := make(map[string]string, len(unpreparedNames)+len(preparedNames))
	for _, name := range unpreparedNames {
		wanted[name] = "worktree_executor.go"
	}
	for _, name := range preparedNames {
		wanted[name] = "worktree_executor_prepared.go"
	}

	owners := functionOwners(t, wanted)
	for name, owner := range wanted {
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestWorktreeExecutorExecutePathReexportIdentity(t *testing.T) {
	// Same-package names stay the execute implementations, so existing cmd
	// call sites keep resolving without import-path changes.
	var (
		_ func(context.Context, cleanupOverlapSafetySelection, cleanupOverlapSafetyRuntime) (cleanExecutionReceipt, error) = executeCleanTargets
		_ func(context.Context, []preparedCleanTarget, activeWorktreeExecutionOptions) (cleanExecutionReceipt, error)      = executePreparedCleanTargets
		_ func() activeWorktreeExecutionOptions                                                                            = defaultActiveWorktreeExecutionOptions
	)
}

func TestExecuteCleanTargetsDoesNotContainPreparedLoopBody(t *testing.T) {
	fset := token.NewFileSet()
	unpreparedFile, err := parser.ParseFile(fset, "worktree_executor.go", nil, 0)
	if err != nil {
		t.Fatalf("parse worktree_executor.go: %v", err)
	}
	unprepared := cmdFunc(unpreparedFile, "executeCleanTargets")
	if unprepared == nil {
		t.Fatal("executeCleanTargets is not defined in worktree_executor.go")
	}

	forbidden := map[string]string{
		"invalidateLastScanCache":           "prepared scan-cache invalidation",
		"ResetRefreshMemo":                  "prepared overlap refresh reset",
		"executePathCleanupTarget":          "prepared path mutation",
		"executeActiveWorktreeUnit":         "prepared active-worktree mutation",
		"cancelledPreparedCleanUnitReceipt": "prepared cancellation receipt",
		"failedPreparedCleanUnitReceipt":    "prepared failure receipt",
		"cleanUnitHasMutation":              "prepared mutation accounting",
		"isActiveWorktreeTarget":            "prepared active-worktree dispatch",
		"removeWorktree":                    "prepared worktree remover",
		"removeAll":                         "prepared path remover",
	}
	var leaked []string
	called := map[string]int{}
	ast.Inspect(unprepared.Body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if reason, ok := forbidden[ident.Name]; ok {
			leaked = append(leaked, reason+" ("+ident.Name+")")
		}
		switch ident.Name {
		case "executePreparedCleanTargets", "prepareCleanExecutionWithSafety", "defaultActiveWorktreeExecutionOptions":
			called[ident.Name]++
		}
		return true
	})
	for _, reason := range leaked {
		t.Errorf("executeCleanTargets body leaked %s", reason)
	}
	if called["executePreparedCleanTargets"] != 1 {
		t.Errorf("executeCleanTargets calls executePreparedCleanTargets %d times; want 1", called["executePreparedCleanTargets"])
	}
	if called["prepareCleanExecutionWithSafety"] != 1 {
		t.Errorf("executeCleanTargets calls prepareCleanExecutionWithSafety %d times; want 1", called["prepareCleanExecutionWithSafety"])
	}
	if called["defaultActiveWorktreeExecutionOptions"] != 1 {
		t.Errorf("executeCleanTargets calls defaultActiveWorktreeExecutionOptions %d times; want 1", called["defaultActiveWorktreeExecutionOptions"])
	}

	preparedFile, err := parser.ParseFile(fset, "worktree_executor_prepared.go", nil, 0)
	if err != nil {
		t.Fatalf("parse worktree_executor_prepared.go: %v", err)
	}
	prepared := cmdFunc(preparedFile, "executePreparedCleanTargets")
	if prepared == nil {
		t.Fatal("executePreparedCleanTargets is not defined in worktree_executor_prepared.go")
	}
	if rangeStmtCount(prepared.Body) == 0 {
		t.Error("executePreparedCleanTargets no longer owns the prepared target loop")
	}
	preparedIdents := identSet(prepared.Body)
	for _, name := range []string{
		"invalidateLastScanCache",
		"ResetRefreshMemo",
		"executePathCleanupTarget",
		"executeActiveWorktreeUnit",
		"cancelledPreparedCleanUnitReceipt",
		"failedPreparedCleanUnitReceipt",
		"cleanUnitHasMutation",
		"isActiveWorktreeTarget",
	} {
		if !preparedIdents[name] {
			t.Errorf("executePreparedCleanTargets no longer uses %s", name)
		}
	}
	if preparedIdents["prepareCleanExecutionWithSafety"] {
		t.Error("executePreparedCleanTargets leaked the unprepared prepare path")
	}
}

func cmdFunc(file *ast.File, name string) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != name {
			continue
		}
		return fn
	}
	return nil
}

func identSet(body *ast.BlockStmt) map[string]bool {
	seen := make(map[string]bool)
	ast.Inspect(body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			seen[ident.Name] = true
		}
		return true
	})
	return seen
}

func rangeStmtCount(body *ast.BlockStmt) int {
	var count int
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.RangeStmt); ok {
			count++
		}
		return true
	})
	return count
}
