// Package confirminput reads confirmation lines without blocking cancellation.
package confirminput

import (
	"bufio"
	"context"
)

// Scan gives one goroutine exclusive ownership of scanner until its read ends.
// Calls must be sequential. On cancellation the caller must abandon the scanner
// and its reader: a generic Reader cannot be interrupted, so the goroutine may
// remain blocked until input, EOF, a read error, or process exit. The reader's
// owner may close it to release the read; Scan never closes caller-owned stdin.
// The buffered result lets the goroutine exit even after the caller returns.
func Scan(ctx context.Context, scanner *bufio.Scanner) (line string, ok bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
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
		return "", false, ctx.Err()
	case value := <-result:
		// Cancellation wins over input that becomes ready at the same boundary.
		if err := ctx.Err(); err != nil {
			return "", false, err
		}
		return value.line, value.ok, value.err
	}
}
