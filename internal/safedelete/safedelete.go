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
//     is itself a directory) and is not inside Git metadata.
//
// The path must already be clean: a "link/../x" path would be judged on one
// location and removed at another once the symlink is resolved.
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
	".relay", ".relay/worktrees", ".gstack", ".gstack/worktrees",
	".config/superpowers", ".config/superpowers/worktrees",
}

// homeEnv names environment variables that relocate an agent home. The homes
// they name, and their stores, are protected wherever they live.
var homeEnv = []string{"CODEX_HOME", "AIBRIS_CODEX_HOMES", "CLAUDE_CONFIG_DIR"}

// relocatedHomeStores are protected below every relocated agent home.
var relocatedHomeStores = []string{"worktrees", "sessions", "projects"}

// Check returns nil when path may be removed, or an error wrapping
// ErrRefused that says why not.
func Check(home, path string) error {
	if path != filepath.Clean(path) {
		return refuse(path, "path is not clean")
	}
	rel, canonical, err := homeRel(home, path)
	if err != nil {
		return refuse(path, err.Error())
	}
	key := foldCase(filepath.ToSlash(rel))
	for _, p := range protected {
		if coversProtected(key, foldCase(p)) {
			return refuse(path, "protected location")
		}
	}
	for _, p := range relocatedHomes() {
		if coversProtected(foldCase(filepath.ToSlash(canonical)), foldCase(filepath.ToSlash(p))) {
			return refuse(path, "agent home or store")
		}
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".git" {
			return refuse(path, "Git metadata")
		}
	}
	if info, err := os.Lstat(filepath.Join(canonical, ".git")); err == nil && info.IsDir() {
		return refuse(path, "primary Git repository")
	}
	return nil
}

// coversProtected reports whether target is the protected path or one of its
// ancestors. Both are slash-separated and case-folded.
func coversProtected(target, protected string) bool {
	return target == protected || strings.HasPrefix(protected, target+"/")
}

// relocatedHomes returns the canonical agent homes named by homeEnv and their
// stores. Relative or unresolvable entries are ignored: they cannot widen
// what is allowed, only fail to add protection the defaults already give.
func relocatedHomes() []string {
	var out []string
	for _, name := range homeEnv {
		for _, entry := range filepath.SplitList(os.Getenv(name)) {
			if entry == "" || !filepath.IsAbs(entry) {
				continue
			}
			entry = canonicalize(entry)
			out = append(out, entry)
			for _, store := range relocatedHomeStores {
				out = append(out, filepath.Join(entry, store))
			}
		}
	}
	return out
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
// and the canonical path.
func homeRel(home, path string) (string, string, error) {
	if home == "" || !filepath.IsAbs(home) {
		return "", "", errors.New("home directory is not absolute")
	}
	if !filepath.IsAbs(path) {
		return "", "", errors.New("path is not absolute")
	}
	canonical := canonicalize(path)
	rel, ok := within(canonicalize(home), canonical)
	if !ok {
		return "", "", errors.New("outside the home directory")
	}
	return rel, canonical, nil
}

// canonicalize resolves symlinks in the longest existing prefix of path and
// appends the rest, so paths that do not exist yet still compare against
// resolved ones (macOS /var is /private/var).
func canonicalize(path string) string {
	path = filepath.Clean(path)
	var rest []string
	for dir := path; ; {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, rest[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		rest = append(rest, filepath.Base(dir))
		dir = parent
	}
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
