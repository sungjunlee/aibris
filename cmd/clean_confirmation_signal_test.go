package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanConfirmationSignals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support sending POSIX SIGINT/SIGTERM to a child; context cancellation is tested separately")
	}
	binary := buildCLIContractBinary(t)
	for _, route := range []string{"final", "per-item", "guided", "unified", "guided-final", "guided-partial"} {
		for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
			t.Run(fmt.Sprintf("%s/%s", route, signal), func(t *testing.T) {
				resetCleanFlags()
				home := t.TempDir()
				testutil.SetHome(t, home)
				args := []string{"clean"}
				var answers []guidedCleanPromptAnswer
				paths := []string{}
				receiptPath := ""
				if strings.HasPrefix(route, "guided") || route == "unified" {
					count := 1
					if route == "guided-partial" {
						count = 2
					}
					paths = append(paths, saveGuidedReceiptCleanFixture(t, home, "signal", count)...)
					args = append(args, "--guide")
					if route != "guided" {
						answers = append(answers, guidedCleanPromptAnswer{prompt: "q to abort: ", reply: "\n"})
					}
					if route == "guided-final" || route == "guided-partial" {
						receiptPath = filepath.Join(t.TempDir(), "receipt.json")
						args = append(args, "--receipt-file", receiptPath)
					}
				}
				if route == "final" || route == "per-item" || route == "unified" {
					modules := filepath.Join(home, "workspace", "signal", "node_modules")
					writeJSONReceiptFixture(t, modules, "sentinel")
					if route == "unified" {
						chtimesTree(t, modules, time.Now().Add(-30*24*time.Hour))
					}
					paths = append(paths, modules)
					if route == "unified" {
						args = append(args, "--category=worktree,node_modules")
						cache, ok := readLastScanCache()
						if !ok {
							t.Fatal("missing fixture cache")
						}
						info, err := os.Stat(modules)
						if err != nil {
							t.Fatal(err)
						}
						saveCleanCacheFixture(t, home, append(cache.Result.Worktrees, types.DebrisInfo{Path: modules, ID: "modules", Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, ModTime: info.ModTime(), Size: info.Size()}))
					} else {
						args = append(args, "--no-guide", "--category=node_modules", "--age=1h")
					}
				}
				prompt := "Proceed? [y/N]: "
				switch route {
				case "per-item":
					args = append(args, "--interactive")
					prompt = "Remove? [y/N]: "
				case "guided":
					prompt = "q to abort: "
				case "unified":
					prompt = "Enter numbers to toggle, Enter to preview, q to abort: "
				case "guided-partial":
					args = append(args, "--interactive")
					answers = append(answers, guidedCleanPromptAnswer{prompt: "Remove? [y/N]: ", reply: "y\n"})
					prompt = "Remove? [y/N]: "
				}
				output := cancelCleanAtPrompt(t, binary, home, args, answers, prompt, signal)
				removed := 0
				for _, path := range paths {
					if _, err := os.Stat(path); os.IsNotExist(err) {
						removed++
					} else if err != nil {
						t.Fatal(err)
					}
				}
				wantRemoved := 0
				if route == "guided-partial" {
					wantRemoved = 1
				}
				if removed != wantRemoved {
					t.Fatalf("removed %d targets; want %d; output=%s", removed, wantRemoved, output)
				}
				if route == "final" || route == "per-item" || route == "unified" {
					payload, err := os.ReadFile(filepath.Join(paths[len(paths)-1], "payload"))
					if err != nil || string(payload) != "sentinel" {
						t.Fatalf("pending sentinel changed: %q, %v", payload, err)
					}
				}
				if receiptPath != "" {
					document := decodeCleanReceiptFile(t, receiptPath)
					wantStatus := cleanJSONReceiptCancelled
					if wantRemoved > 0 {
						wantStatus = cleanJSONReceiptPartialFailure
					}
					if document["status"] != wantStatus {
						t.Fatalf("receipt status=%v; want %s", document["status"], wantStatus)
					}
					totals := jsonReceiptObject(t, document, "totals")
					assertCleanReceiptRequestAccounting(t, totals)
					if wantRemoved > 0 && jsonReceiptInt64(totals, "freed_bytes") <= 0 {
						t.Fatalf("previous removal accounting lost: %+v", totals)
					}
					if jsonReceiptInt(totals, "removed") != wantRemoved || jsonReceiptInt(totals, "cancelled") != 1 {
						t.Fatalf("cancellation totals=%+v", totals)
					}
				}
			})
		}
	}
}

func cancelCleanAtPrompt(t *testing.T, binary, home string, args []string, answers []guidedCleanPromptAnswer, prompt string, signal os.Signal) string {
	t.Helper()
	child := exec.Command(binary, args...)
	child.Env = cliContractEnv(os.Environ(), home)
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	child.Stderr = &stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	defer func() {
		// Reap only this test's child, including on a prompt or signal failure.
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	ready := make(chan error, 1)
	transcript := make(chan string, 1)
	go func() {
		prefix, err := answerGuidedCleanPrompts(stdout, stdin, append(answers, guidedCleanPromptAnswer{prompt: prompt}))
		ready <- err
		rest, _ := io.ReadAll(stdout)
		transcript <- prefix + string(rest)
	}()
	select {
	case err := <-ready:
		if err != nil {
			output := <-transcript
			_ = child.Wait()
			t.Fatalf("prompt barrier: %v; output=%s; stderr=%s", err, output, stderr.String())
		}
	case <-time.After(20 * time.Second):
		_ = child.Process.Kill()
		output := <-transcript
		_ = child.Wait()
		t.Fatalf("child did not reach confirmation; stdout=%s; stderr=%s", output, stderr.String())
	}
	if err := child.Process.Signal(signal); err != nil {
		t.Fatal(err)
	}
	select {
	case output := <-transcript:
		_ = child.Wait()
		return output
	case <-time.After(2 * time.Second):
		_ = child.Process.Kill()
		<-transcript
		_ = child.Wait()
		t.Fatal("child remained blocked after cancellation at confirmation")
		return ""
	}
}
