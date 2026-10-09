package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Run the actual CLI in a subprocess so flag parsing and deferred cleanup have
// the same lifecycle as a standalone invocation, without building the harness.
func TestLifecycleProcess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[2] != "--" {
		return
	}
	os.Args = append([]string{"perfharness"}, os.Args[3:]...)
	flag.CommandLine = flag.NewFlagSet("perfharness", flag.ExitOnError)
	main()
	os.Exit(0)
}

func lifecycleCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestLifecycleProcess$", "--"}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func lifecycleRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, root)
	}
	isolateHome(t, filepath.Join(root, "home"))
	// Keep all build and temporary data inside the fixture and disable network
	// access and operator Go/Git configuration.
	for key, value := range map[string]string{
		"GOCACHE": filepath.Join(root, "go-cache"), "GOPATH": filepath.Join(root, "go-path"),
		"GOPROXY": "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local", "GOFLAGS": "",
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.DevNull,
		"GIT_AUTHOR_NAME": "fixture", "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
		"GIT_COMMITTER_NAME": "fixture", "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
	} {
		t.Setenv(key, value)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init")
	write("go.mod", "module fixture\n\ngo 1.26.3\n")
	write("main.go", "package main\nimport \"fmt\"\nfunc main() { fmt.Println(`{\"worktrees\":[],\"summary\":{}}`) }\n")
	git("add", ".")
	git("-c", "commit.gpgsign=false", "commit", "-m", "working fixture")
	git("branch", "working")
	write("main.go", "package main\nfunc main() { undefined() }\n")
	git("add", ".")
	git("-c", "commit.gpgsign=false", "commit", "-m", "broken fixture")
	return repo
}

func lifecycleParent(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "sibling"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"sentinel", filepath.Join("sibling", "sentinel")} {
		if err := os.WriteFile(filepath.Join(parent, path), []byte("keep me"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return parent
}

func assertLifecycleParent(t *testing.T, parent string) []os.DirEntry {
	t.Helper()
	for _, path := range []string{"sentinel", filepath.Join("sibling", "sentinel")} {
		got, err := os.ReadFile(filepath.Join(parent, path))
		if err != nil || string(got) != "keep me" {
			t.Errorf("parent content %s changed: %q, %v", path, got, err)
		}
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("parent was removed: %v", err)
	}
	return entries
}

func TestRunWorkdirOwnership(t *testing.T) {
	repo := lifecycleRepo(t)
	for _, tc := range []struct {
		name, base, wantError string
	}{
		{"normal", "working", ""},
		{"invalid-ref", "missing-ref", "resolving ref"},
		{"build-failure", "HEAD", "building base"},
	} {
		for _, keep := range []bool{false, true} {
			name := tc.name + "/cleanup"
			if keep {
				name = tc.name + "/keep"
			}
			t.Run(name, func(t *testing.T) {
				parent := lifecycleParent(t)
				args := []string{"-repo", repo, "-base", tc.base, "-change", "working", "-workdir", parent, "-quick", "-pairs", "1"}
				if keep {
					args = append(args, "-keep")
				}
				out, err := lifecycleCLI(t, args...)
				if tc.wantError == "" && err != nil || tc.wantError != "" && (err == nil || !strings.Contains(out, tc.wantError)) {
					t.Fatalf("unexpected CLI result: %v\n%s", err, out)
				}
				entries := assertLifecycleParent(t, parent)
				if !keep {
					if len(entries) != 2 {
						t.Fatalf("run artifacts remain in parent: %v", entries)
					}
					return
				}
				if len(entries) != 3 {
					t.Fatalf("want only sentinel, sibling and one run child; got %v", entries)
				}
				for _, entry := range entries {
					if entry.Name() == "sentinel" || entry.Name() == "sibling" {
						continue
					}
					child := filepath.Join(parent, entry.Name())
					if !entry.IsDir() || !strings.Contains(out, "kept working directory: "+child+"\n") {
						t.Fatalf("kept child not reported: %s\n%s", child, out)
					}
				}
			})
		}
	}
}

func TestRunDefaultWorkdirOwnership(t *testing.T) {
	repo := lifecycleRepo(t)
	parent := lifecycleParent(t)
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, parent)
	}
	for _, keep := range []bool{true, false} {
		args := []string{"-repo", repo, "-base", "missing-ref", "-change", "working"}
		if keep {
			args = append(args, "-keep")
		}
		out, err := lifecycleCLI(t, args...)
		if err == nil || !strings.Contains(out, "resolving ref") {
			t.Fatalf("want invalid ref error: %v\n%s", err, out)
		}
		entries := assertLifecycleParent(t, parent)
		// The earlier kept child must survive the next run's cleanup too.
		if len(entries) != 3 {
			t.Fatalf("want sentinel, sibling and one kept child: %v", entries)
		}
		if keep {
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "aibris-perfharness-") && !strings.Contains(out, "kept working directory: "+filepath.Join(parent, entry.Name())+"\n") {
					t.Fatalf("kept child not reported: %s", out)
				}
			}
		}
	}
}

func TestRunChildCreationFailure(t *testing.T) {
	repo := lifecycleRepo(t)
	for _, mode := range []string{"explicit-parent", "default-child"} {
		t.Run(mode, func(t *testing.T) {
			parent := lifecycleParent(t)
			// A file is a portable obstruction: permissions tests are unreliable as root.
			blocked := filepath.Join(parent, "sentinel")
			args := []string{"-repo", repo, "-base", "working", "-change", "working"}
			wantError := "creating workdir parent"
			if mode == "explicit-parent" {
				args = append(args, "-workdir", blocked)
			} else {
				for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
					t.Setenv(key, blocked)
				}
				wantError = "creating run temp directory"
			}
			out, err := lifecycleCLI(t, args...)
			if err == nil || !strings.Contains(out, wantError) || strings.Contains(out, "building base") {
				t.Fatalf("want failure before building: %v\n%s", err, out)
			}
			if entries := assertLifecycleParent(t, parent); len(entries) != 2 {
				t.Fatalf("parent changed: %v", entries)
			}
		})
	}
}
