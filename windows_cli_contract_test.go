//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCLIContractBinaryCommands(t *testing.T) {
	if !strings.EqualFold(filepath.Ext(cliContractBinary), ".exe") {
		t.Fatalf("CLI contract binary = %q; want a Windows .exe", cliContractBinary)
	}

	home := filepath.Join(t.TempDir(), "profile")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "version", args: []string{"--version"}, want: "aibris version"},
		{name: "help", args: []string{"--help"}, want: "Usage:"},
		{name: "scan", args: []string{"scan", "--json"}, want: `"worktrees"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runCLIContract(t, home, nil, test.args...)
			if result.ExitCode != 0 {
				t.Fatalf("aibris %v exit = %d\nstdout:\n%s\nstderr:\n%s",
					test.args, result.ExitCode, result.Stdout, result.Stderr)
			}
			if !strings.Contains(result.Stdout, test.want) {
				t.Fatalf("aibris %v stdout missing %q:\n%s",
					test.args, test.want, result.Stdout)
			}
		})
	}
}

func TestWindowsCLIContractRootContainmentAndHomeIsolation(t *testing.T) {
	fixtureRoot := t.TempDir()
	home := filepath.Join(fixtureRoot, "profile")
	contained := filepath.Join(home, "workspace")
	siblingPrefix := filepath.Join(fixtureRoot, "profile-sibling")
	outsideHome := t.TempDir()

	insideModules := filepath.Join(contained, "inside", "node_modules")
	siblingModules := filepath.Join(siblingPrefix, "sibling", "node_modules")
	outsideModules := filepath.Join(outsideHome, "outside", "node_modules")
	for _, path := range []string{insideModules, siblingModules, outsideModules} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	worktreeOwner := filepath.Join(home, ".codex", "worktrees", "windows-contract")
	writeCLIContractFixture(
		t,
		filepath.Join(worktreeOwner, ".git"),
		"gitdir: "+filepath.Join(
			home,
			"missing-parent-repo",
			".git",
			"worktrees",
			"windows-contract",
		)+"\n",
	)

	t.Setenv("HOME", outsideHome)
	t.Setenv("USERPROFILE", siblingPrefix)
	command := newCLIContractCommand(t, t.Context(), home, nil, "scan", "--json")
	for _, key := range []string{"HOME", "USERPROFILE"} {
		count := 0
		for _, entry := range command.Env {
			envKey, value, _ := strings.Cut(entry, "=")
			if !strings.EqualFold(envKey, key) {
				continue
			}
			count++
			if value != home {
				t.Fatalf("%s = %q; want isolated profile %q", key, value, home)
			}
		}
		if count != 1 {
			t.Fatalf("%s entries = %d, want exactly one: %q", key, count, command.Env)
		}
	}

	decodeScan := func(result cliContractResult) []struct {
		Path string `json:"path"`
	} {
		t.Helper()
		var scan struct {
			Worktrees []struct {
				Path string `json:"path"`
			} `json:"worktrees"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
			t.Fatalf("decode Windows scan: %v\n%s", err, result.Stdout)
		}
		return scan.Worktrees
	}

	defaultScan := runCLIContract(t, home, nil, "scan", "--json")
	if defaultScan.ExitCode != 0 {
		t.Fatalf("isolated default scan exit = %d\nstdout:\n%s\nstderr:\n%s",
			defaultScan.ExitCode, defaultScan.Stdout, defaultScan.Stderr)
	}
	foundDefault := map[string]bool{}
	for _, item := range decodeScan(defaultScan) {
		path := filepath.Clean(item.Path)
		rel, err := filepath.Rel(home, path)
		if err != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(rel) {
			t.Fatalf("isolated default scan escaped profile %q with path %q", home, item.Path)
		}
		foundDefault[path] = true
	}
	for _, want := range []string{insideModules, worktreeOwner} {
		if !foundDefault[filepath.Clean(want)] {
			t.Fatalf("isolated default scan did not report %q:\n%s", want, defaultScan.Stdout)
		}
	}
	for _, escaped := range []string{siblingModules, outsideModules} {
		if strings.Contains(defaultScan.Stdout, escaped) {
			t.Fatalf("isolated default scan leaked outside fixture path %q:\n%s",
				escaped, defaultScan.Stdout)
		}
	}

	result := runCLIContract(t, home, nil, "scan", "--root", contained, "--json")
	if result.ExitCode != 0 {
		t.Fatalf("contained root exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	foundInside := false
	for _, item := range decodeScan(result) {
		rel, err := filepath.Rel(home, filepath.Clean(item.Path))
		if err != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(rel) {
			t.Fatalf("isolated scan escaped profile %q with path %q", home, item.Path)
		}
		if filepath.Clean(item.Path) == filepath.Clean(insideModules) {
			foundInside = true
		}
	}
	if !foundInside {
		t.Fatalf("contained scan did not report %q:\n%s", insideModules, result.Stdout)
	}
	for _, escaped := range []string{siblingModules, outsideModules} {
		if strings.Contains(result.Stdout, escaped) {
			t.Fatalf("contained scan leaked outside fixture path %q:\n%s", escaped, result.Stdout)
		}
	}

	for _, test := range []struct {
		name string
		root string
	}{
		{name: "sibling prefix", root: siblingPrefix},
		{name: "outside home", root: outsideHome},
	} {
		t.Run(test.name, func(t *testing.T) {
			rejected := runCLIContract(t, home, nil, "scan", "--root", test.root, "--json")
			if rejected.ExitCode == 0 {
				t.Fatalf("outside root %q succeeded:\n%s", test.root, rejected.Stdout)
			}
			if !strings.Contains(rejected.Stderr, "must be under") {
				t.Fatalf("outside root %q error missing containment refusal:\n%s",
					test.root, rejected.Stderr)
			}
		})
	}

	tildeBackslash := runCLIContract(t, home, nil, "scan", "--root", `~\workspace`, "--json")
	if tildeBackslash.ExitCode == 0 {
		t.Fatalf(`unsupported "~\workspace" root succeeded: %s`, tildeBackslash.Stdout)
	}
	if !strings.Contains(tildeBackslash.Stderr, "must be absolute or start with ~") {
		t.Fatalf(`unsupported "~\workspace" root error was unclear: %s`, tildeBackslash.Stderr)
	}
}
