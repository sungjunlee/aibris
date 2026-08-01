package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Binary is an immutable built binary under test, exported read-only from a git
// ref and built exactly once. It is never rebuilt during a measurement run, per
// the frozen four-pair protocol's immutable-inputs requirement.
type Binary struct {
	Label     string // "base" or "change"
	SourceRef string // git ref the source was exported from
	SourceSHA string // resolved git commit SHA the tree was exported at
	Path      string // path to the built binary
	SHA256    string // hex sha256 of the binary bytes
	WorkDir   string // exported source tree (retained for inspection/cleanup)
}

// BuildBinary exports the tree at ref via `git archive`, builds it once with
// `go build -trimpath`, and records the binary's SHA-256. The ref is exported
// read-only; it is never checked out, rebased, or otherwise mutated, so a
// frozen branch (e.g. the parked #139 L2 branch) can serve as the change input
// without being touched.
func BuildBinary(label, ref, repoDir, outDir string) (*Binary, error) {
	sha, err := gitRevParse(repoDir, ref)
	if err != nil {
		return nil, fmt.Errorf("resolving ref %q: %w", ref, err)
	}
	srcDir := filepath.Join(outDir, label+"-src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return nil, err
	}
	if err := gitArchive(repoDir, sha, srcDir); err != nil {
		return nil, fmt.Errorf("exporting %s: %w", sha, err)
	}
	binPath := filepath.Join(outDir, "aibris-"+label)
	if err := goBuildTrimpath(srcDir, binPath); err != nil {
		return nil, fmt.Errorf("building %s: %w", label, err)
	}
	sum, err := fileSHA256(binPath)
	if err != nil {
		return nil, err
	}
	return &Binary{
		Label:     label,
		SourceRef: ref,
		SourceSHA: sha,
		Path:      binPath,
		SHA256:    sum,
		WorkDir:   srcDir,
	}, nil
}

// gitRevParse resolves ref to a commit SHA.
func gitRevParse(repoDir, ref string) (string, error) {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", ref+"^{commit}")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// gitArchive pipes `git archive --format=tar <sha>` into `tar -x -C dest`,
// exporting the exact tree at sha without a checkout.
func gitArchive(repoDir, sha, dest string) error {
	arch := exec.Command("git", "-C", repoDir, "archive", "--format=tar", sha)
	tar := exec.Command("tar", "-x", "-C", dest)
	pr, pw := io.Pipe()
	arch.Stdout = pw
	tar.Stdin = pr
	var archErr, tarErr bytes.Buffer
	arch.Stderr = &archErr
	tar.Stderr = &tarErr
	if err := tar.Start(); err != nil {
		_ = pw.Close()
		return err
	}
	archRunErr := arch.Run()
	_ = pw.Close()
	if archRunErr != nil {
		_ = tar.Wait()
		return fmt.Errorf("%w: %s", archRunErr, strings.TrimSpace(archErr.String()))
	}
	if err := tar.Wait(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(tarErr.String()))
	}
	return nil
}

// goBuildTrimpath builds the module rooted at srcDir into outBin with -trimpath
// so the binary is independent of the build directory path.
func goBuildTrimpath(srcDir, outBin string) error {
	cmd := exec.Command("go", "build", "-trimpath", "-o", outBin, ".")
	cmd.Dir = srcDir
	cmd.Env = os.Environ()
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return nil
}

// fileSHA256 returns the hex sha256 of a file's contents.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
