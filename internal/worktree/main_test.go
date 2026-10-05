package worktree

import (
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Real Git fixtures under parallel package load can exceed the production
	// bound; a timeout there is a correct fail-closed result, not a test signal.
	GitEvidenceCommandTimeout = time.Minute
	os.Exit(m.Run())
}
