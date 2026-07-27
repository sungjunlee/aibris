package adapter

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestAgentStateStoreClassifiersRouteThroughSharedRecordedCWDDecision(t *testing.T) {
	tests := []struct {
		source   string
		function string
	}{
		{source: "claude_projects.go", function: "classifyClaudeProjectEntry"},
		{source: "cursor.go", function: "classifyCursorProjectEntry"},
	}
	for _, tt := range tests {
		t.Run(tt.function, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), tt.source, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			var sharedCalls int
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Name.Name != tt.function {
					continue
				}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					identifier, ok := call.Fun.(*ast.Ident)
					if ok && identifier.Name == "classifyRecordedCWDEntry" {
						sharedCalls++
					}
					return true
				})
			}
			if sharedCalls != 1 {
				t.Fatalf("%s calls classifyRecordedCWDEntry %d times; want exactly once",
					tt.function, sharedCalls)
			}
		})
	}
}
