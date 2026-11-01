//go:build unix

package main

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestHashTreeNamedPipeFailsWithoutReading(t *testing.T) {
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := hashTree(root, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("named pipe was accepted as a measurable input")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hashTree blocked reading a non-regular entry")
	}
}
