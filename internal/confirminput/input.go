// Package confirminput reads confirmation lines without blocking cancellation.
package confirminput

import (
	"bufio"
	"context"
	"io"
)

// Reader preserves buffered lines across sequential confirmation prompts.
// Create one per command run and pass it to every prompt that reads that input.
// After cancellation it permanently refuses further reads, even with a fresh
// context. A generic io.Reader cannot be interrupted: the outstanding goroutine
// may remain blocked until input, EOF, a read error, or process exit. The run
// must stop on cancellation; it cannot resume prompting on the same input.
// The input's owner may close it to release the read; Scan never closes stdin.
type Reader struct {
	scanner   *bufio.Scanner
	cancelErr error
}

func NewReader(input io.Reader) *Reader {
	return &Reader{scanner: bufio.NewScanner(input)}
}

// Scan gives one goroutine exclusive ownership of the scanner until its read
// ends. Calls must be sequential. The buffered result lets the goroutine exit
// even after a cancelled caller returns; cancelErr prevents a subsequent prompt
// from racing that goroutine or having its answer stolen by the abandoned read.
func Scan(ctx context.Context, input *Reader) (line string, ok bool, err error) {
	if input.cancelErr != nil {
		return "", false, input.cancelErr
	}
	if err := ctx.Err(); err != nil {
		input.cancelErr = err
		return "", false, err
	}
	scanner := input.scanner
	type scanResult struct {
		line string
		ok   bool
		err  error
	}
	result := make(chan scanResult, 1)
	go func() {
		ok := scanner.Scan()
		result <- scanResult{scanner.Text(), ok, scanner.Err()}
	}()
	select {
	case <-ctx.Done():
		input.cancelErr = ctx.Err()
		return "", false, input.cancelErr
	case value := <-result:
		// Cancellation wins over input that becomes ready at the same boundary.
		if err := ctx.Err(); err != nil {
			input.cancelErr = err
			return "", false, err
		}
		return value.line, value.ok, value.err
	}
}
