// Command citestjson reads `go test -json` on stdin and exits non-zero when
// the selection was empty or a test failed. Skips are printed separately from
// passes so a skipped regression is not evidence that it ran.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/sungjunlee/aibris/internal/citestjson"
)

func main() {
	summary, err := citestjson.SummarizeWithOutput(os.Stdin, os.Stdout)
	fmt.Println(summary)
	if err == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "citestjson: %v\n", err)
	if errors.Is(err, citestjson.ErrNoTests) {
		os.Exit(2)
	}
	os.Exit(1)
}
