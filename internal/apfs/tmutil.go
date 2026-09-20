package apfs

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// PurgeBytes is the bounded thin request. It is not a delete of Time Machine
// backups on an external disk.
const PurgeBytes = 20 * 1024 * 1024 * 1024
const Urgency = "4"

func FormatError(args []string, err error, out []byte) error {
	name := commandName(args)
	if msg := sanitizeOutput(out); msg != "" {
		return fmt.Errorf("%s: %w\n%s", name, err, msg)
	}
	return fmt.Errorf("%s: %w", name, err)
}

func commandName(args []string) string {
	if len(args) == 0 {
		return "tmutil"
	}
	return "tmutil " + args[0]
}

func sanitizeOutput(out []byte) string {
	var kept []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if keepOutputLine(s) {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "\n")
}

func keepOutputLine(s string) bool {
	return s != "" && !strings.HasPrefix(s, "Snapshots for") && !isSnapshotID(s)
}

func isSnapshotID(s string) bool {
	if strings.HasPrefix(s, "com.apple.TimeMachine.") || strings.HasPrefix(s, "com.apple.os.update-") {
		return true
	}
	return containsLocalSnapshotStamp(s)
}

func containsLocalSnapshotStamp(s string) bool {
	for i := 0; i+17 <= len(s); i++ {
		if isLocalSnapshotStamp(s[i : i+17]) {
			return true
		}
	}
	return false
}

func isLocalSnapshotStamp(s string) bool {
	_, err := time.Parse("2006-01-02-150405", s)
	return err == nil && len(s) == 17
}

func ParseCount(output []byte) int {
	n := 0
	for _, line := range bytes.Split(output, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if s == "" || strings.HasPrefix(s, "Snapshots for") {
			continue
		}
		n++
	}
	return n
}
