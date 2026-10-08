package test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func runInstallSnippet(t *testing.T, home, script string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-c", script, "bash"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", cmdArgs...)
	cmd.Dir = "."
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=/usr/bin:/bin",
		"SHELL=/bin/zsh",
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("script timed out: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	return string(out)
}

func runInstallSnippetWithoutHome(t *testing.T, script string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"-c", script, "bash"}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", cmdArgs...)
	cmd.Dir = "."
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"SHELL=/bin/zsh",
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("script timed out: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestInstallScriptRunsFromStdin(t *testing.T) {
	t.Helper()
	script, err := os.Open("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer script.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-s", "--", "--help")
	cmd.Dir = "."
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"PATH=/usr/bin:/bin",
		"SHELL=/bin/zsh",
	}
	cmd.Stdin = script
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("script timed out: %v\n%s", ctx.Err(), out)
	}
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Install aibris.") {
		t.Fatalf("stdin execution did not print usage; output:\n%s", out)
	}
}

func TestInstallScriptDefaultDirIsUserLocal(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
if [[ -z "$INSTALL_DIR" ]]; then
  INSTALL_DIR="$(default_install_dir)"
fi
printf 'dir=%s\nexplicit=%s\n' "$INSTALL_DIR" "$INSTALL_DIR_EXPLICIT"
`)

	if !strings.Contains(output, "dir="+filepath.Join(home, ".local", "bin")) {
		t.Fatalf("default install dir not user-local; output:\n%s", output)
	}
	if !strings.Contains(output, "explicit=0") {
		t.Fatalf("default install dir should not be explicit; output:\n%s", output)
	}
}

func TestInstallScriptExplicitPrefixDoesNotRequireHome(t *testing.T) {
	output := runInstallSnippetWithoutHome(t, `
source ./install.sh
parse_args --prefix /usr/local/bin 0.6.0
INSTALL_DIR="$(expand_path "$INSTALL_DIR")"
printf 'dir=%s\nexplicit=%s\nversion=%s\n' "$INSTALL_DIR" "$INSTALL_DIR_EXPLICIT" "$VERSION"
`)

	if !strings.Contains(output, "dir=/usr/local/bin") {
		t.Fatalf("explicit prefix was not preserved; output:\n%s", output)
	}
	if !strings.Contains(output, "explicit=1") {
		t.Fatalf("prefix should mark install dir explicit; output:\n%s", output)
	}
	if !strings.Contains(output, "version=0.6.0") {
		t.Fatalf("version argument not parsed; output:\n%s", output)
	}
}

func TestInstallScriptPrefixIsExplicitAndExpandsHome(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
parse_args --prefix '~/bin' 0.6.0
INSTALL_DIR="$(expand_path "$INSTALL_DIR")"
printf 'dir=%s\nexplicit=%s\nversion=%s\n' "$INSTALL_DIR" "$INSTALL_DIR_EXPLICIT" "$VERSION"
`)

	if !strings.Contains(output, "dir="+filepath.Join(home, "bin")) {
		t.Fatalf("prefix was not expanded under HOME; output:\n%s", output)
	}
	if !strings.Contains(output, "explicit=1") {
		t.Fatalf("prefix should mark install dir explicit; output:\n%s", output)
	}
	if !strings.Contains(output, "version=0.6.0") {
		t.Fatalf("version argument not parsed; output:\n%s", output)
	}
}

func TestInstallScriptPathHintUsesHomeVariable(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
INSTALL_DIR="$HOME/.local/bin"
print_path_hint
`)

	for _, want := range []string{
		"aibris was installed to ~/.local/bin",
		`echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc`,
		`export PATH="$HOME/.local/bin:$PATH"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("path hint missing %q; output:\n%s", want, output)
		}
	}
}

func TestInstallScriptPathHintSkipsWhenAlreadyOnPath(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
INSTALL_DIR="$HOME/.local/bin"
PATH="$INSTALL_DIR:/usr/bin:/bin"
print_path_hint
`)

	if output != "" {
		t.Fatalf("expected no PATH hint when install dir is already on PATH; output:\n%s", output)
	}
}

func TestInstallFromMainStampsCommitIdentifyingVersion(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(t.TempDir(), "src")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=aibris", "GIT_AUTHOR_EMAIL=dev@example.com",
			"GIT_COMMITTER_NAME=aibris", "GIT_COMMITTER_EMAIL=dev@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init", "-q")
	run("git", "checkout", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("stamp"), 0644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "README")
	run("git", "commit", "-q", "-m", "stamp")
	shaOut, err := exec.Command("git", "-C", repo, "rev-parse", "--short=12", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(shaOut))

	output := runInstallSnippet(t, home, `
source ./install.sh
main_version_ldflags "$1"
`, repo)
	want := "-X github.com/sungjunlee/aibris/cmd.version=main-" + sha
	if !strings.Contains(output, want) {
		t.Fatalf("ldflags missing %q; output:\n%s", want, output)
	}
	if strings.Contains(output, "cmd.version=dev") {
		t.Fatalf("main install still stamps bare dev; output:\n%s", output)
	}
}

func TestInstallScriptZshFpathHintWhenNotOnFpath(t *testing.T) {
	home := t.TempDir()
	output := runInstallSnippet(t, home, `
source ./install.sh
print_zsh_fpath_hint
`)
	want := "zsh: add ~/.local/share/zsh/site-functions to fpath before compinit; see docs/COMPLETIONS.md"
	if !strings.Contains(output, want) {
		t.Fatalf("zsh fpath hint missing %q; output:\n%s", want, output)
	}
	if strings.Contains(output, ".zshrc") {
		t.Fatalf("installer must not edit or instruct writing .zshrc; output:\n%s", output)
	}
}

func TestCompletionsDocDocumentsZshFpathSnippet(t *testing.T) {
	data, err := os.ReadFile("docs/COMPLETIONS.md")
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"fpath",
		`fpath=("$HOME/.local/share/zsh/site-functions" $fpath)`,
		"before",
		"compinit",
		"never edits",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("COMPLETIONS.md missing %q", want)
		}
	}
}

func TestInstallScriptInstallBinaryToDefaultDirWithoutSudo(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "aibris")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nprintf 'aibris test\\n'\n"), 0755); err != nil {
		t.Fatal(err)
	}

	output := runInstallSnippet(t, home, `
source ./install.sh
if [[ -z "$INSTALL_DIR" ]]; then
  INSTALL_DIR="$(default_install_dir)"
fi
INSTALL_DIR="$(expand_path "$INSTALL_DIR")"
install_binary "$1"
"$INSTALL_DIR/aibris"
`, source)

	if strings.Contains(output, "Using sudo") {
		t.Fatalf("default user-local install should not use sudo; output:\n%s", output)
	}
	if !strings.Contains(output, "Installed aibris to "+filepath.Join(home, ".local", "bin", "aibris")) {
		t.Fatalf("install output missing destination; output:\n%s", output)
	}
	if !strings.Contains(output, "aibris test") {
		t.Fatalf("installed binary did not run; output:\n%s", output)
	}
}

// installFixture isolates the child process and keeps a sentinel next to the
// destination so cleanup cannot silently remove the installation directory.
type installFixture struct {
	home, temp, prefix, release string
}

func newInstallFixture(t *testing.T) installFixture {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	f := installFixture{home: home, temp: filepath.Join(home, "tmp"), prefix: filepath.Join(home, "prefix"), release: filepath.Join(home, "release")}
	for _, dir := range []string{f.temp, f.prefix, f.release} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeInstallFile(t, filepath.Join(f.prefix, "sentinel"), []byte("keep parent"), 0644)
	return f
}

func writeInstallFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func assertInstallContents(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s bytes changed: got %q, want %q", path, got, want)
	}
}

func (f installFixture) assertClean(t *testing.T, binary string) {
	t.Helper()
	assertInstallContents(t, filepath.Join(f.prefix, "sentinel"), []byte("keep parent"))
	for _, dir := range []string{f.temp, f.prefix} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if dir == f.prefix && (entry.Name() == binary || entry.Name() == "sentinel") {
				continue
			}
			t.Errorf("installer left unexpected file: %s", filepath.Join(dir, entry.Name()))
		}
	}
}

var installTestBinary = []byte("#!/bin/sh\nprintf 'aibris 0.12.1\\n'\n")

func (f installFixture) unixRelease(t *testing.T, failure string) {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	name := "aibris"
	if failure == "binary-missing" {
		name = "README"
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(installTestBinary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(installTestBinary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	data := archive.Bytes()
	if failure == "malformed-archive" {
		data = []byte("not a tar archive")
	}
	// Supply every supported filename; production still chooses OS and arch.
	var checksums strings.Builder
	for _, platform := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			asset := "aibris_" + platform + "_" + arch + ".tar.gz"
			writeInstallFile(t, filepath.Join(f.release, asset), data, 0644)
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			if failure == "checksum-mismatch" {
				hash = strings.Repeat("0", 64)
			}
			if failure != "checksum-missing" {
				fmt.Fprintf(&checksums, "%s  %s\n", hash, asset)
			}
		}
	}
	writeInstallFile(t, filepath.Join(f.release, "checksums.txt"), []byte(checksums.String()), 0644)
}

func (f installFixture) runUnix(t *testing.T, version, failure string) (string, error) {
	t.Helper()
	// Only transport is replaced on normal paths. Mutation faults operate on
	// the path production passes to install/mv, including partial writes.
	script := `
source "$1"
release="$2"
failure="$3"
curl() {
  local output="" url=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      -o) output="$2"; shift 2 ;;
      -*) shift ;;
      *) url="$1"; shift ;;
    esac
  done
  if [[ "$failure" == "download" || ( "$failure" == "archive-download" && "$url" != */checksums.txt ) ]]; then
    [[ -z "$output" ]] || printf 'partial download' > "$output"
    return 22
  fi
  [[ -n "$output" ]] || return 22
  cp "$release/${url##*/}" "$output"
}
if [[ "$failure" == "stage-write" ]]; then
  install() {
    printf 'partial binary' > "${@: -1}"
    printf 'injected stage write failure\n' >&2
    return 1
  }
fi
if [[ "$failure" == "replacement" ]]; then
  mv() {
    printf 'injected replacement failure\n' >&2
    return 1
  }
fi
main --prefix "$4" "$5"
`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script, "bash", "./install.sh", f.release, failure, f.prefix, version)
	cmd.Env = []string{"HOME=" + f.home, "USERPROFILE=" + f.home, "LOCALAPPDATA=" + filepath.Join(f.home, ".cache"),
		"TMPDIR=" + f.temp, "TEMP=" + f.temp, "TMP=" + f.temp, "PATH=/usr/bin:/bin", "SHELL=/bin/sh"}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("installer timed out: %v\n%s", ctx.Err(), out)
	}
	return string(out), err
}

func TestInstallReleaseFailuresPreserveExisting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer; native Windows has PowerShell tests")
	}
	for _, version := range []string{"0.12.1", "latest"} {
		t.Run(version, func(t *testing.T) {
			for _, tc := range []struct{ failure, message string }{
				{"download", ""},
				{"archive-download", "release archive not found"},
				{"checksum-missing", "checksum for"},
				{"checksum-mismatch", "checksum mismatch"},
				{"malformed-archive", "tar:"},
				{"binary-missing", "aibris not found in archive"},
				{"stage-write", "injected stage write failure"},
				{"replacement", "injected replacement failure"},
			} {
				t.Run(tc.failure, func(t *testing.T) {
					f := newInstallFixture(t)
					f.unixRelease(t, tc.failure)
					existing := []byte("existing binary\x00\xff")
					binary := filepath.Join(f.prefix, "aibris")
					writeInstallFile(t, binary, existing, 0755)
					output, err := f.runUnix(t, version, tc.failure)
					if err == nil {
						t.Errorf("installer succeeded on %s; output:\n%s", tc.failure, output)
					} else if _, ok := err.(*exec.ExitError); !ok {
						t.Fatalf("installer did not run: %v", err)
					}
					message := tc.message
					if version == "latest" && tc.failure == "archive-download" {
						message = "could not resolve latest GitHub Release"
					}
					if !strings.Contains(output, message) {
						t.Errorf("failure did not reach expected boundary %q:\n%s", message, output)
					}
					assertInstallContents(t, binary, existing)
					f.assertClean(t, "aibris")
				})
			}
		})
	}
}

func TestInstallReleaseFirstInstallAndSameVersionRerun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix installer; native Windows has PowerShell tests")
	}
	for _, version := range []string{"0.12.1", "latest"} {
		t.Run(version, func(t *testing.T) {
			f := newInstallFixture(t)
			f.unixRelease(t, "")
			for i := 0; i < 2; i++ {
				output, err := f.runUnix(t, version, "")
				if err != nil {
					t.Fatalf("install %d failed: %v\n%s", i+1, err, output)
				}
				assertInstallContents(t, filepath.Join(f.prefix, "aibris"), installTestBinary)
				info, err := os.Stat(filepath.Join(f.prefix, "aibris"))
				if err != nil || info.Mode().Perm()&0111 == 0 {
					t.Fatalf("installed binary is not executable: %v", err)
				}
				f.assertClean(t, "aibris")
			}
		})
	}
}
