package cmd

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestScanProgressPrinter_InteractiveSummary(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "scan-progress")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	progress := &scanProgressPrinter{
		out:         out,
		interactive: true,
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
		active:      make(map[types.Tool]bool),
	}
	go progress.spin()

	progress.Handle(types.ScanProgressEvent{State: types.ScanProgressStart, Tool: types.ToolNodeModules})
	progress.Handle(types.ScanProgressEvent{State: types.ScanProgressStart, Tool: types.ToolCodex})
	progress.Handle(types.ScanProgressEvent{
		State: types.ScanProgressDone,
		Tool:  types.ToolNodeModules,
		Count: 2,
		Size:  2048,
	})
	progress.Handle(types.ScanProgressEvent{
		State: types.ScanProgressError,
		Tool:  types.ToolCodex,
		Err:   errors.New("boom"),
	})
	progress.Stop()

	if _, err := out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	output := string(raw)
	for _, want := range []string{"\x1b[2K", "scanned  2 sources", "2 items", "2.0 KB", "1 errors"} {
		if !strings.Contains(output, want) {
			t.Errorf("progress output missing %q; got: %q", want, output)
		}
	}
}

func TestActiveToolsSortsAndTruncates(t *testing.T) {
	got := activeTools(map[types.Tool]bool{
		types.ToolWindsurf:    true,
		types.ToolCodex:       true,
		types.ToolNodeModules: true,
		types.ToolBuildCache:  true,
	})
	want := "build-cache, node_modules, windsurf..."
	if got != want {
		t.Errorf("activeTools() = %q; want %q", got, want)
	}
}

func TestScanProgressLabel_WorktreeAdapterIsNotCodex(t *testing.T) {
	if got := scanProgressLabel(types.ToolCodex); got != "worktree" {
		t.Fatalf("scanProgressLabel(codex) = %q; want worktree", got)
	}
	if got := scanProgressLabel(types.ToolNodeModules); got != "node_modules" {
		t.Fatalf("scanProgressLabel(node_modules) = %q; want node_modules", got)
	}

	output := captureOutput(func() {
		printScanProgress(types.ScanProgressEvent{
			State: types.ScanProgressDone,
			Tool:  types.ToolCodex,
			Count: 4,
			Size:  1024,
		})
	})
	if !strings.Contains(output, "found    worktree") {
		t.Fatalf("progress missing worktree label; got: %q", output)
	}
	if strings.Contains(output, "found    codex") {
		t.Fatalf("progress still attributes worktree rows to codex; got: %q", output)
	}
}
