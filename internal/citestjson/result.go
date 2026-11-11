// Package citestjson reads `go test -json` output and separates tests that
// ran from tests that were skipped. An empty selection is not a successful run.
package citestjson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNoTests means the test command selected nothing. `go test` exits 0 in
// that case; callers must not treat it as evidence that a regression ran.
var ErrNoTests = errors.New("no tests selected")

// ErrFailed means a test or package failed.
var ErrFailed = errors.New("go test failed")

// Summary counts test-level JSON events. Package-level events are not tests.
// A skipped test increments Skipped and is not a pass.
type Summary struct {
	Selected      int
	Passed        int
	Failed        int
	Skipped       int
	PackageFailed bool
	NoTestsToRun  bool
}

func (s Summary) String() string {
	return fmt.Sprintf("selected=%d passed=%d failed=%d skipped=%d", s.Selected, s.Passed, s.Failed, s.Skipped)
}

// Summarize consumes `go test -json` events. A non-JSON line is an error so
// a build failure that leaked onto stdout cannot look like an empty success.
func Summarize(r io.Reader) (Summary, error) {
	return summarize(r, io.Discard)
}

// SummarizeWithOutput consumes test events while replaying their human-readable
// output. This keeps failing names, assertion details, race reports, panic
// stacks, and skip reasons visible when go test -json is piped through a
// selected-test checker.
func SummarizeWithOutput(r io.Reader, output io.Writer) (Summary, error) {
	return summarize(r, output)
}

func summarize(r io.Reader, output io.Writer) (Summary, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var summary Summary
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if line[0] != '{' {
			return summary, fmt.Errorf("citestjson: non-JSON test output: %s", truncate(line, 200))
		}
		var event struct {
			Action string
			Test   string
			Output string
		}
		if err := json.Unmarshal(line, &event); err != nil {
			return summary, fmt.Errorf("citestjson: %w", err)
		}
		if event.Output != "" {
			if _, err := io.WriteString(output, event.Output); err != nil {
				return summary, err
			}
		}
		if strings.Contains(event.Output, "no tests to run") {
			summary.NoTestsToRun = true
		}
		if event.Test == "" {
			if event.Action == "fail" {
				summary.PackageFailed = true
			}
			continue
		}
		switch event.Action {
		case "run":
			summary.Selected++
		case "pass":
			summary.Passed++
		case "fail":
			summary.Failed++
		case "skip":
			summary.Skipped++
		}
	}
	if err := scanner.Err(); err != nil {
		return summary, err
	}
	return summary, summary.err()
}

func (s Summary) err() error {
	switch {
	case s.Failed > 0 || s.PackageFailed:
		return fmt.Errorf("%w: %s", ErrFailed, s)
	case s.Selected == 0 || s.NoTestsToRun:
		return fmt.Errorf("%w: %s", ErrNoTests, s)
	default:
		return nil
	}
}

func truncate(line []byte, n int) string {
	if len(line) <= n {
		return string(line)
	}
	return string(line[:n]) + "..."
}
