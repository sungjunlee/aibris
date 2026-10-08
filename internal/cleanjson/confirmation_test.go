package cleanjson

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/confirminput"
)

func TestJSONConfirmationInput(t *testing.T) {
	for _, input := range []string{"y\n", "n\n", "", "invalid\n"} {
		approved, cancelled := readConfirmation(t.Context(), confirminput.NewReader(strings.NewReader(input)))
		if approved != (input == "y\n") || cancelled != (input == "") {
			t.Fatalf("input %q = approved %t, cancelled %t", input, approved, cancelled)
		}
	}
}

func TestJSONConfirmationCancellation(t *testing.T) {
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		approved, cancelled := readConfirmation(ctx, confirminput.NewReader(&jsonConfirmationBarrier{Reader: input, started: started}))
		done <- !approved && cancelled
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("confirmation did not read")
	}
	cancel()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("JSON confirmation accepted cancelled input")
		}
	case <-time.After(time.Second):
		t.Fatal("JSON confirmation ignored cancellation")
	}
}

type jsonConfirmationBarrier struct {
	io.Reader
	started chan struct{}
}

func (r *jsonConfirmationBarrier) Read(p []byte) (int, error) {
	close(r.started)
	return r.Reader.Read(p)
}

type jsonConfirmationCancellingReader struct{ cancel context.CancelFunc }

func (r jsonConfirmationCancellingReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "y\n"), io.EOF
}

func TestJSONConfirmationCancellationWinsInput(t *testing.T) {
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithCancel(t.Context())
		approved, cancelled := readConfirmation(ctx, confirminput.NewReader(jsonConfirmationCancellingReader{cancel}))
		cancel()
		if approved || !cancelled {
			t.Fatalf("input during cancellation = approved %t, cancelled %t", approved, cancelled)
		}
	}
}
