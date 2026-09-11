package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Repository identity is the canonical Git common-dir. These helpers resolve
// gitdir and commondir paths and do not decide cleanup eligibility.

func HasGitWorktreeMetadata(path string) bool {
	gitFilePath := filepath.Join(path, ".git")
	info, err := os.Lstat(gitFilePath)
	if err != nil || (!info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0) {
		return false
	}
	data, err := os.ReadFile(gitFilePath)
	if err != nil {
		return false
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if !strings.HasPrefix(line, "gitdir: ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "gitdir: ")) != ""
}

func resolveRepositoryIdentity(worktreePath string) (string, string, error) {
	gitFilePath := filepath.Join(worktreePath, ".git")
	gitDirValue, err := readSingleGitMetadataPath(gitFilePath, "gitdir: ")
	if err != nil {
		return "", "", err
	}
	gitDirPath := gitDirValue
	if !filepath.IsAbs(gitDirPath) {
		gitDirPath = filepath.Join(worktreePath, gitDirPath)
	}
	canonicalGitDir, err := canonicalGitDirectory(gitDirPath)
	if err != nil {
		return "", "", fmt.Errorf("unreadable Git metadata: resolving git-dir %q: %w", gitDirPath, err)
	}

	commonDirPath := canonicalGitDir
	commonDirFile := filepath.Join(canonicalGitDir, "commondir")
	if _, err := os.Lstat(commonDirFile); err == nil {
		commonDirValue, err := readSingleGitMetadataPath(commonDirFile, "")
		if err != nil {
			return "", "", err
		}
		commonDirPath = commonDirValue
		if !filepath.IsAbs(commonDirPath) {
			commonDirPath = filepath.Join(canonicalGitDir, commonDirPath)
		}
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("unreadable Git metadata: inspecting %q: %w", commonDirFile, err)
	}

	canonicalCommonDir, err := canonicalGitDirectory(commonDirPath)
	if err != nil {
		return "", "", fmt.Errorf("unreadable Git metadata: resolving common-dir %q: %w", commonDirPath, err)
	}
	return canonicalCommonDir, displayRepositoryName(canonicalCommonDir), nil
}

func readSingleGitMetadataPath(path, prefix string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unreadable Git metadata: reading %q: %w", path, err)
	}

	value := strings.TrimSpace(string(content))
	lines := strings.Split(value, "\n")
	if value == "" || len(lines) != 1 {
		return "", fmt.Errorf("ambiguous Git metadata: %q must contain exactly one path", path)
	}
	line := strings.TrimSpace(strings.TrimSuffix(lines[0], "\r"))
	if prefix != "" {
		if !strings.HasPrefix(line, prefix) {
			return "", fmt.Errorf("ambiguous Git metadata: %q does not contain a valid %sentry", path, prefix)
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	if line == "" {
		return "", fmt.Errorf("ambiguous Git metadata: %q contains an empty path", path)
	}
	return line, nil
}

func canonicalGitDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	return filepath.Clean(resolved), nil
}

func displayRepositoryName(commonDir string) string {
	if filepath.Base(commonDir) == ".git" {
		return filepath.Base(filepath.Dir(commonDir))
	}
	return filepath.Base(commonDir)
}
