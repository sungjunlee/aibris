package cleaner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("AIBRIS_FAKE_CMD"); mode != "" {
		code, err := runFakeCommand(mode)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(99)
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// The copied test binary acts as a tool before testing parses the tool's argv.
// No shell or real package manager is reachable through the fixture PATH.
type fakeCommand struct {
	mode     string
	file     string
	content  string
	exitCode int
}

func writeExecutable(t *testing.T, path string, command fakeCommand) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		path += ".exe"
		t.Setenv("PATHEXT", ".EXE")
	}
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	t.Setenv("PATH", filepath.Dir(path))
	t.Setenv("AIBRIS_FAKE_CMD", command.mode)
	t.Setenv("AIBRIS_FAKE_CMD_FILE", command.file)
	t.Setenv("AIBRIS_FAKE_CMD_CONTENT", command.content)
	t.Setenv("AIBRIS_FAKE_CMD_EXIT", strconv.Itoa(command.exitCode))
	// A sentinel that cannot be resolved would let refusal/no-subprocess tests
	// pass without exercising their intended authority check.
	resolved, err := exec.LookPath(strings.TrimSuffix(filepath.Base(path), ".exe"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(resolved)
	if err != nil || !os.SameFile(want, got) {
		t.Fatalf("fake executable resolved to %q, want %q: %v", resolved, path, err)
	}
}

func runFakeCommand(mode string) (int, error) {
	file := os.Getenv("AIBRIS_FAKE_CMD_FILE")
	switch mode {
	case "exit":
		if content := os.Getenv("AIBRIS_FAKE_CMD_CONTENT"); content != "" {
			fmt.Fprintln(os.Stdout, content)
		}
		return strconv.Atoi(os.Getenv("AIBRIS_FAKE_CMD_EXIT"))
	case "write-file":
		return 0, os.WriteFile(file, []byte(os.Getenv("AIBRIS_FAKE_CMD_CONTENT")), 0o644)
	case "record-argv":
		return 0, os.WriteFile(file, []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o644)
	case "reject-force":
		for _, arg := range os.Args[1:] {
			if arg == "--force" {
				fmt.Fprintln(os.Stdout, "error: unexpected argument '--force' found")
				return 2, nil
			}
		}
		return 0, nil
	case "remove-file":
		err := os.Remove(file)
		if os.IsNotExist(err) {
			err = nil
		}
		return 0, err
	case "sleep":
		time.Sleep(2 * time.Second)
		return 0, nil
	case "uv-record":
		cwd, err := os.Getwd()
		if err != nil {
			return 0, err
		}
		cache := os.Getenv("UV_CACHE_DIR")
		record := append(os.Args[1:], cache, cwd, "")
		if err := os.WriteFile(filepath.Join(cache, "command-record"), []byte(strings.Join(record, "\n")), 0o644); err != nil {
			return 0, err
		}
		if sameFakeCommandPath(cwd, cache) {
			return 7, nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("unknown fake command mode %q", mode)
	}
}

func sameFakeCommandPath(got, want string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(got, want)
	}
	return got == want
}
