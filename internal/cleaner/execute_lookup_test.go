package cleaner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecute_ErrDotDoesNotFallbackToPathRemoval(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := testutil.UVCache(home)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("data")
	payloadPath := filepath.Join(path, "file")
	if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	// The copied executable would delete the payload if a refusal were turned
	// into a successful relative run. Lookup must fail before that.
	writeExecutable(t, filepath.Join(binDir, "uv"), fakeCommand{mode: "remove-file", file: payloadPath})
	t.Chdir(binDir)
	t.Setenv("PATH", ".")
	if _, err := exec.LookPath("uv"); !errors.Is(err, exec.ErrDot) {
		t.Fatalf("LookPath(uv) = %v; want exec.ErrDot", err)
	}

	var output, errorOutput bytes.Buffer
	total, err := ExecuteWithContextAndBarrierWithOutput(
		context.Background(),
		[]types.DebrisInfo{{
			ID:             "uv",
			Tool:           types.ToolPipCache,
			Category:       types.CategoryOtherCache,
			Path:           path,
			Size:           int64(len(payload)),
			CleanupKind:    types.CleanupCommand,
			CleanupCommand: []string{"uv", "cache", "clean"},
		}},
		nil,
		&output,
		&errorOutput,
	)
	if !errors.Is(err, exec.ErrDot) {
		t.Fatalf("err = %v; want exec.ErrDot\nstdout=%s\nstderr=%s", err, output.String(), errorOutput.String())
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0 freed bytes", total)
	}
	got, readErr := os.ReadFile(payloadPath)
	if readErr != nil {
		t.Fatalf("payload = %v; want it untouched", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q; want %q", got, payload)
	}
	if strings.Contains(output.String(), "falling back") || strings.Contains(errorOutput.String(), "falling back") || strings.Contains(output.String(), "removed:") {
		t.Fatalf("relative lookup refusal took the path-removal path\nstdout=%s\nstderr=%s", output.String(), errorOutput.String())
	}
}
