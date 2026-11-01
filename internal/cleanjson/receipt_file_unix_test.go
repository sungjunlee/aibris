//go:build unix

package cleanjson

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrepareReceiptSinkRejectsNamedPipeWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// No writer opens the FIFO. A blocking O_RDONLY open would wait forever
	// and ignore cancellation, so the probe has to return on its own.
	done := make(chan error, 1)
	go func() {
		_, err := PrepareReceiptSink(path, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("FIFO receipt error = %v; want not a regular file", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("receipt probe blocked on a named pipe; want a fast rejection")
	}

	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO after refusal = mode %v, err %v; want the pipe left in place", info.Mode(), err)
	}
}
