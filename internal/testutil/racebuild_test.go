package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestChildBinaryMatchesHarnessRaceMode(t *testing.T) {
	dir := t.TempDir()
	src := "package main\nfunc main() {}\n"
	if RaceEnabled() {
		src = `package main
import "sync"
func main() {
	var x int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { x = 1; wg.Done() }()
	go func() { x = 2; wg.Done() }()
	wg.Wait()
	_ = x
}
`
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "child")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", GoBuildArgs(bin, "main.go")...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	got, err := BinaryHasRace(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != RaceEnabled() {
		t.Fatalf("binary -race=%v; harness -race=%v", got, RaceEnabled())
	}
	if !RaceEnabled() {
		return
	}
	// CLI contract runners treat a non-zero child as a test failure. A race
	// in an instrumented child must take that path.
	out, err := exec.Command(bin).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "DATA RACE") {
		t.Fatalf("race-instrumented child did not fail the run: %v\n%s", err, out)
	}
}
