package confirminput

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestScanLines(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("y\nn\ninvalid\nlast"))
	for _, want := range []string{"y", "n", "invalid", "last"} {
		line, ok, err := Scan(t.Context(), scanner)
		if err != nil || !ok || line != want {
			t.Fatalf("Scan = %q, %t, %v; want %q", line, ok, err, want)
		}
	}
	if _, ok, err := Scan(t.Context(), scanner); ok || err != nil {
		t.Fatalf("EOF = %t, %v", ok, err)
	}
}

func TestScanCancellationReleasesCaller(t *testing.T) {
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &observedReader{Reader: input, started: make(chan struct{}), ended: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, _, err := Scan(ctx, bufio.NewScanner(reader)); result <- err }()
	<-reader.started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input wait ignored cancellation")
	}
	// Closing our reader releases the outstanding read. Never reuse its scanner.
	input.Close()
	select {
	case <-reader.ended:
	case <-time.After(time.Second):
		t.Fatal("owned reader did not terminate after close")
	}
}

type observedReader struct {
	io.Reader
	started chan struct{}
	ended   chan struct{}
}

func (r *observedReader) Read(p []byte) (int, error) {
	close(r.started)
	defer close(r.ended)
	return r.Reader.Read(p)
}

type cancellingReader struct{ cancel context.CancelFunc }

func (r cancellingReader) Read(p []byte) (int, error) { r.cancel(); return copy(p, "y\n"), io.EOF }

func TestScanCancellationWinsArrivingInput(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		line, ok, err := Scan(ctx, bufio.NewScanner(cancellingReader{cancel}))
		cancel()
		if !errors.Is(err, context.Canceled) || ok || line != "" {
			t.Fatalf("cancelled input = %q, %t, %v", line, ok, err)
		}
	}
}

func TestScanAlreadyCancelledDoesNotRead(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, ok, err := Scan(ctx, bufio.NewScanner(panicReader{}))
	if ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled Scan = %t, %v", ok, err)
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("read after cancellation") }

func TestScanReadError(t *testing.T) {
	_, ok, err := Scan(t.Context(), bufio.NewScanner(errorReader{}))
	if ok || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %t, %v", ok, err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
