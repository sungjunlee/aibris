package adapter

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
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

func TestAgentStateStoreClassifiersRecordedCWDAncestorAvailability(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	type storeClassifier struct {
		name     string
		write    func(*testing.T, string, string)
		classify func(context.Context, string) (types.EntryClass, string, string, error)
	}
	stores := []storeClassifier{
		{
			name: "claude",
			write: func(t *testing.T, entryPath, cwd string) {
				writeClaudeProjectSession(t, filepath.Join(entryPath, "session.jsonl"),
					claudeSessionLine(t, cwd)+"\n")
			},
			classify: classifyClaudeProjectEntry,
		},
		{
			name: "cursor",
			write: func(t *testing.T, entryPath, cwd string) {
				writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+cwd+"\n")
			},
			classify: classifyCursorProjectEntry,
		},
	}

	t.Run("mount root is undetermined", func(t *testing.T) {
		ancestor := filepath.Join(home, "mounted-workspace")
		if err := os.MkdirAll(ancestor, 0755); err != nil {
			t.Fatal(err)
		}
		parentInfo, err := os.Lstat(filepath.Dir(ancestor))
		if err != nil {
			t.Fatal(err)
		}
		parentDevice, err := recordedCWDDeviceID(filepath.Dir(ancestor), parentInfo)
		if err != nil {
			t.Fatal(err)
		}
		mountDevice := parentDevice ^ 1

		originalDeviceID := recordedCWDDeviceID
		recordedCWDDeviceID = func(path string, info os.FileInfo) (uint64, error) {
			if path == ancestor {
				return mountDevice, nil
			}
			return originalDeviceID(path, info)
		}
		t.Cleanup(func() {
			recordedCWDDeviceID = originalDeviceID
		})

		recordedCWD := filepath.Join(ancestor, "missing-project")
		for _, store := range stores {
			t.Run(store.name, func(t *testing.T) {
				entryPath := filepath.Join(home, "."+store.name, "projects", "mount-root-entry")
				store.write(t, entryPath, recordedCWD)

				classification, reason, _, err := store.classify(context.Background(), entryPath)
				if err != nil {
					t.Fatal(err)
				}
				if classification != types.EntryClassUndetermined {
					t.Fatalf("Classification = %q; want undetermined; reason: %s",
						classification, reason)
				}
				if !strings.Contains(reason, "surrounding tree is unavailable") ||
					!strings.Contains(reason, ancestor) {
					t.Fatalf("Reason = %q; want unavailable surrounding tree naming ancestor %s",
						reason, ancestor)
				}
			})
		}
	})

	t.Run("symlinked mount root is undetermined", func(t *testing.T) {
		target := filepath.Join(home, "mounted-workspace-target")
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		ancestor := filepath.Join(home, "symlinked-mounted-workspace")
		if err := os.Symlink(target, ancestor); err != nil {
			t.Fatal(err)
		}
		parentInfo, err := os.Lstat(filepath.Dir(ancestor))
		if err != nil {
			t.Fatal(err)
		}
		parentDevice, err := recordedCWDDeviceID(filepath.Dir(ancestor), parentInfo)
		if err != nil {
			t.Fatal(err)
		}
		mountDevice := parentDevice ^ 1

		originalDeviceID := recordedCWDDeviceID
		recordedCWDDeviceID = func(path string, info os.FileInfo) (uint64, error) {
			if path == ancestor {
				if info.Mode()&os.ModeSymlink != 0 {
					return parentDevice, nil
				}
				return mountDevice, nil
			}
			return originalDeviceID(path, info)
		}
		t.Cleanup(func() {
			recordedCWDDeviceID = originalDeviceID
		})

		recordedCWD := filepath.Join(ancestor, "missing-project")
		for _, store := range stores {
			t.Run(store.name, func(t *testing.T) {
				entryPath := filepath.Join(home, "."+store.name, "projects", "symlink-mount-root-entry")
				store.write(t, entryPath, recordedCWD)

				classification, reason, _, err := store.classify(context.Background(), entryPath)
				if err != nil {
					t.Fatal(err)
				}
				if classification != types.EntryClassUndetermined {
					t.Fatalf("Classification = %q; want undetermined; reason: %s",
						classification, reason)
				}
				if !strings.Contains(reason, "surrounding tree is unavailable") ||
					!strings.Contains(reason, ancestor) {
					t.Fatalf("Reason = %q; want unavailable surrounding tree naming ancestor %s",
						reason, ancestor)
				}
			})
		}
	})

	t.Run("ordinary missing directory is orphaned", func(t *testing.T) {
		ancestor := filepath.Join(home, "ordinary-workspace")
		if err := os.MkdirAll(ancestor, 0755); err != nil {
			t.Fatal(err)
		}
		recordedCWD := filepath.Join(ancestor, "missing-parent", "missing-project")

		for _, store := range stores {
			t.Run(store.name, func(t *testing.T) {
				entryPath := filepath.Join(home, "."+store.name, "projects", "ordinary-missing-entry")
				store.write(t, entryPath, recordedCWD)

				classification, reason, _, err := store.classify(context.Background(), entryPath)
				if err != nil {
					t.Fatal(err)
				}
				if classification != types.EntryClassOrphaned {
					t.Fatalf("Classification = %q; want orphaned; reason: %s",
						classification, reason)
				}
			})
		}
	})
}
