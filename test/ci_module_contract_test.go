package test

import (
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestVerificationModuleContract(t *testing.T) {
	ci := readWorkflowContract(t, ".github/workflows/ci.yml")
	check := false
	for _, step := range ci.Jobs["check"].Steps {
		if step.Run == "make check" {
			check = true
		}
	}
	if !check {
		t.Error("shared CI must run the default development check")
	}
	if ci.Jobs["windows-safety"].RunsOn != "windows-latest" || ci.Jobs["test"].RunsOn != "${{ matrix.os }}" {
		t.Error("platform verification must use native Windows and the Linux/macOS matrix")
	}
	if !slices.Contains(ci.Jobs["test"].Strategy.Matrix["os"], "ubuntu-latest") || !slices.Contains(ci.Jobs["test"].Strategy.Matrix["os"], "macos-latest") {
		t.Error("module verification must run on Linux and macOS")
	}
	var runs []string
	for _, step := range ci.Jobs["test"].Steps {
		runs = append(runs, step.Run)
	}
	for _, command := range []string{"go test -race -count=1 -cover ./...", "go -C tools/perfharness test -race -count=1 -cover ./...", "make lint"} {
		if !slices.Contains(runs, command) {
			t.Errorf("Linux/macOS missing module verification: %s", command)
		}
	}
	windows := false
	for _, step := range ci.Jobs["windows-safety"].Steps {
		if strings.Contains(step.Run, "go test -race -count=1 -cover ./internal/safedelete ./internal/pathidentity ./internal/testutil") && !strings.Contains(step.Run, "-run") {
			windows = true
		}
	}
	if !windows {
		t.Error("Windows must run whole deletion-gate, identity, and hermeticity packages")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Makefile is a Unix developer entry point; native Windows uses explicit package commands")
	}
	for _, target := range []string{"test", "test-race", "lint", "check"} {
		out, err := exec.Command("make", "-n", target).CombinedOutput()
		if err != nil {
			t.Fatalf("make -n %s: %v\n%s", target, err, out)
		}
		commands := string(out)
		if target == "lint" || target == "check" {
			for _, command := range []string{"go -C tools/perfharness vet ./...", "go -C tools/perfharness run honnef.co/go/tools/cmd/staticcheck@"} {
				if !strings.Contains(commands, command) {
					t.Errorf("make %s missing %s", target, command)
				}
			}
		} else {
			if !strings.Contains(commands, "go -C tools/perfharness test ") || !strings.Contains(commands, "go test ") {
				t.Errorf("make %s must test both modules", target)
			}
		}
	}
}
