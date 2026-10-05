// Package safedelete is the single gate every destructive filesystem
// operation in aibris passes through.
//
// Providers decide what is debris and the cleanup pipeline decides what is
// selected; this package only answers "may anything ever be removed at this
// path?". Its rules are deliberately independent of provider logic, so a bug
// in discovery or selection cannot turn into deleting a user's documents,
// credentials, a tool's whole home, or a primary Git repository.
//
// Rules, applied to the canonical path (symlinks resolved):
//
//  1. The path is strictly inside the home directory.
//  2. The path is not a protected location and not an ancestor of one.
//  3. The path is not a primary Git repository (a directory whose .git entry
//     is itself a directory).
//
// No other package calls os.RemoveAll; an architecture test enforces it.
package safedelete

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrRefused marks every refusal so callers can tell a safety refusal apart
// from an I/O failure.
var ErrRefused = errors.New("refused by the deletion safety gate")

// protected lists home-relative locations that no cleanup target can be,
// written with forward slashes. A target equal to one of these, or an
// ancestor of one, is refused. Subdirectories of a protected location stay
// eligible: ~/Library/Caches/Homebrew is a cleanup target, ~/Library is not.
var protected = []string{
	// User content.
	"Desktop", "Documents", "Downloads", "Pictures", "Movies", "Music",
	"Public", "Applications", "OneDrive", "Dropbox", "Google Drive",
	// Platform state.
	"Library", "Library/Application Support", "Library/Preferences",
	"Library/Keychains", "Library/Mobile Documents", "Library/CloudStorage",
	"Library/Mail", "Library/Messages", "Library/Containers",
	"Library/Group Containers", "Library/Caches", "Library/Developer",
	"AppData", "AppData/Local", "AppData/Roaming", "AppData/LocalLow",
	// Credentials and shell configuration.
	".ssh", ".gnupg", ".aws", ".azure", ".kube", ".docker", ".password-store",
	".config", ".local", ".local/bin", ".local/share", ".local/state",
	// Tool and language homes. Their caches are targets, the homes are not.
	".cache", ".npm", ".gradle", ".cargo", ".rustup", "go", ".pub-cache",
	".codex", ".claude", ".cursor", ".codeium", ".gemini", ".vscode",
	// Agent stores and worktree containers: their entries are targets, the
	// store or container itself never is.
	".codex/worktrees", ".codex/sessions", ".claude/projects", ".cursor/projects",
}

// Check returns nil when path may be removed, or an error wrapping
// ErrRefused that says why not.
func Check(home, path string) error {
	rel, canonical, err := homeRel(home, path)
	if err != nil {
		return refuse(path, err.Error())
	}
	key := foldCase(filepath.ToSlash(rel))
	for _, p := range protected {
		p = foldCase(p)
		if key == p || strings.HasPrefix(p, key+"/") {
			return refuse(path, "protected location")
		}
	}
	if info, err := os.Lstat(filepath.Join(canonical, ".git")); err == nil && info.IsDir() {
		return refuse(path, "primary Git repository")
	}
	return nil
}

// RemoveAll removes path after Check passes.
func RemoveAll(home, path string) error {
	if err := Check(home, path); err != nil {
		return err
	}
	return os.RemoveAll(path)
}

// RemoveAllUnderHome is RemoveAll against the current user's home directory.
// It matches the func(string) error shape that executors inject.
func RemoveAllUnderHome(path string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return refuse(path, "home directory unavailable")
	}
	return RemoveAll(home, path)
}

// RemoveAllIn removes rel inside an opened root after Check passes for the
// absolute path it names. Removal through the root cannot leave it even if a
// directory on the way is swapped for a symlink after the check.
func RemoveAllIn(root *os.Root, home, path, rel string) error {
	if err := Check(home, path); err != nil {
		return err
	}
	return root.RemoveAll(rel)
}

func refuse(path, reason string) error {
	return fmt.Errorf("%w: %q: %s", ErrRefused, path, reason)
}

// homeRel returns path relative to home after resolving symlinks in both,
// and the canonical path. A path that does not exist yet is resolved through
// home's canonical form so a symlinked home still compares correctly.
func homeRel(home, path string) (string, string, error) {
	if home == "" || !filepath.IsAbs(home) {
		return "", "", errors.New("home directory is not absolute")
	}
	if !filepath.IsAbs(path) {
		return "", "", errors.New("path is not absolute")
	}
	rawHome := filepath.Clean(home)
	path = filepath.Clean(path)
	canonicalHome := rawHome
	if resolved, err := filepath.EvalSymlinks(rawHome); err == nil {
		canonicalHome = filepath.Clean(resolved)
	}
	canonical := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonical = filepath.Clean(resolved)
	} else if rel, ok := within(rawHome, path); ok {
		canonical = filepath.Join(canonicalHome, rel)
	}
	rel, ok := within(canonicalHome, canonical)
	if !ok {
		return "", "", errors.New("outside the home directory")
	}
	return rel, canonical, nil
}

// within reports path relative to root when path is strictly below root.
func within(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if foldCase(filepath.Join(root, rel)) != foldCase(path) {
		return "", false
	}
	return rel, true
}

// foldCase lowercases on platforms whose default filesystems are
// case-insensitive, so ~/library cannot slip past ~/Library.
func foldCase(s string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}
