// Package safedelete is the single gate every destructive filesystem
// operation in aibris passes through.
//
// Providers decide what is debris and the cleanup pipeline decides what is
// selected; this package only answers "may anything ever be removed at this
// path?". Its rules are deliberately independent of provider logic, so a bug
// in discovery or selection cannot turn into deleting a user's documents,
// credentials, a tool's whole home, or a primary Git repository.
//
// Rules, applied to the lexical and canonical paths (links resolved):
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

	"github.com/sungjunlee/aibris/internal/codexhome"
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
	".codex", ".claude", ".cursor", ".codeium", ".gemini", ".grok", ".vscode",
	// Agent stores and worktree containers: their entries are targets, the
	// store or container itself never is.
	".codex/worktrees", ".codex/sessions", ".claude/projects", ".cursor/projects",
	".grok/sessions",
	".relay", ".relay/worktrees", ".gstack", ".gstack/worktrees",
	".config/superpowers", ".config/superpowers/worktrees",
	"orca", "orca/workspaces",
}

// relocatedHome names an environment variable that relocates an agent home
// and the stores below it. The home and its stores are protected wherever
// they live; entries inside a store stay eligible.
type relocatedHome struct {
	env    string
	stores []string
}

var relocatedHomeEnv = []relocatedHome{
	{env: "CLAUDE_CONFIG_DIR", stores: []string{"projects"}},
}

// Check returns nil when path may be removed, or an error wrapping
// ErrRefused that says why not.
func Check(home, path string) error {
	if path != filepath.Clean(path) {
		return refuse(path, "path is not clean")
	}
	// Resolve HOME before joining protected locations: cleaning a spelling
	// such as link/../home first could protect a different directory.
	home, err := canonicalize(home)
	if err != nil {
		return refuse(path, err.Error())
	}
	rel, canonical, err := homeRel(home, path)
	if err != nil {
		return refuse(path, err.Error())
	}
	key := foldCase(filepath.ToSlash(rel))
	// Identity is cached only within this Check. A later mutation check must
	// observe links and directory identities again.
	identities := make(map[string]os.FileInfo)
	candidateInfo, candidateTail, err := existingPathIdentity(canonical, identities)
	if err != nil {
		return refuse(path, "target identity unavailable: "+err.Error())
	}
	locations := relocatedHomes()
	for _, p := range protected {
		locations = append(locations, filepath.Join(home, filepath.FromSlash(p)))
	}
	for _, location := range locations {
		// Resolve the parent separately to protect the link's physical location
		// as well as its referent, even through a relocated home or container.
		parent, err := canonicalize(filepath.Dir(location))
		if err != nil {
			return refuse(path, "protected location unavailable: "+err.Error())
		}
		resolved, err := canonicalize(location)
		if err != nil {
			return refuse(path, "protected location unavailable: "+err.Error())
		}
		physical := filepath.Join(parent, filepath.Base(location))
		for _, target := range []string{path, canonical} {
			for _, form := range []string{location, physical, resolved} {
				if coversProtected(foldCase(filepath.ToSlash(target)), foldCase(filepath.ToSlash(form))) {
					return refuse(path, "protected location")
				}
			}
		}
		// Filesystems can treat different Unicode spellings as the same
		// directory. Compare actual identities without merging distinct names
		// on filesystems where those spellings name different objects.
		for _, form := range []string{physical, resolved} {
			covered, err := coversProtectedIdentity(candidateInfo, candidateTail, form, identities)
			if err != nil {
				return refuse(path, "protected identity unavailable: "+err.Error())
			}
			if covered {
				return refuse(path, "protected location")
			}
		}
	}
	// Orca's immediate repository directories are containers, never owners.
	// Protect this depth without relying on a mutable directory inventory.
	orca, err := canonicalize(filepath.Join(home, "orca", "workspaces"))
	if err != nil {
		return refuse(path, "protected worktree container unavailable: "+err.Error())
	}
	if foldCase(filepath.Dir(canonical)) == foldCase(orca) {
		return refuse(path, "protected worktree container")
	}
	// Both spellings: a symlinked .git resolves to a name without ".git".
	for _, form := range []string{key, foldCase(filepath.ToSlash(path))} {
		for _, part := range strings.Split(form, "/") {
			if part == ".git" {
				return refuse(path, "Git metadata")
			}
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

// pathIdentityAt caches successful lookups and proven absence for one Check.
func pathIdentityAt(path string, identities map[string]os.FileInfo) (os.FileInfo, error) {
	if info, known := identities[path]; known {
		return info, nil
	}
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	identities[path] = info
	return info, nil
}

// existingPathIdentity binds a path's existing prefix to its missing suffix.
// This preserves protection when two aliases reach the same absent child.
func existingPathIdentity(path string, identities map[string]os.FileInfo) (os.FileInfo, string, error) {
	tail := ""
	for current := path; ; current = filepath.Dir(current) {
		info, err := pathIdentityAt(current, identities)
		if err != nil {
			return nil, "", err
		}
		if info != nil {
			return info, tail, nil
		}
		if filepath.Dir(current) == current {
			return nil, "", errors.New("path has no existing ancestor")
		}
		tail = filepath.Join(filepath.Base(current), tail)
	}
}

// coversProtectedIdentity checks the protected path and its ancestors. A
// missing candidate suffix must still contain the corresponding protected
// suffix; sharing an existing parent alone does not authorize protection.
func coversProtectedIdentity(candidate os.FileInfo, candidateTail, location string, identities map[string]os.FileInfo) (bool, error) {
	tail := ""
	for current := location; ; current = filepath.Dir(current) {
		info, err := pathIdentityAt(current, identities)
		if err != nil {
			return false, err
		}
		if info != nil && os.SameFile(candidate, info) &&
			(candidateTail == "" || coversProtected(foldCase(filepath.ToSlash(candidateTail)), foldCase(filepath.ToSlash(tail)))) {
			return true, nil
		}
		if filepath.Dir(current) == current {
			return false, nil
		}
		tail = filepath.Join(filepath.Base(current), tail)
	}
}

// relocatedHomes returns configured Codex homes and environment-relocated agent
// homes with their stores. Relative entries are ignored: they cannot widen
// what is allowed, only fail to add protection the defaults already give.
func relocatedHomes() []string {
	var out []string
	homes, err := codexhome.Homes()
	if err != nil {
		// A missing default primary home must not drop explicit protections.
		homes = codexhome.ExtraHomes()
	}
	for _, home := range homes {
		if !filepath.IsAbs(home) {
			continue
		}
		out = append(out, home, filepath.Join(home, "worktrees"), filepath.Join(home, "sessions"))
	}
	for _, home := range relocatedHomeEnv {
		entry := strings.TrimSpace(os.Getenv(home.env))
		if entry == "" || !filepath.IsAbs(entry) {
			continue
		}
		out = append(out, entry)
		for _, store := range home.stores {
			out = append(out, filepath.Join(entry, store))
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
	canonical, err := canonicalize(path)
	if err != nil {
		return "", "", err
	}
	canonicalHome, err := canonicalize(home)
	if err != nil {
		return "", "", err
	}
	rel, ok := within(canonicalHome, canonical)
	if !ok {
		return "", "", errors.New("outside the home directory")
	}
	return rel, canonical, nil
}

// canonicalize resolves each existing component, including Windows junctions.
// Missing components remain lexical, so absent paths and dangling links still
// protect their intended destinations. All other lookup failures are errors:
// falling back to the input spelling would discard protection evidence.
func canonicalize(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path is not absolute")
	}
	volume := filepath.VolumeName(path)
	resolved := volume + string(filepath.Separator)
	parts := strings.Split(filepath.ToSlash(path[len(volume):]), "/")
	links := 0
	for len(parts) > 0 {
		part := parts[0]
		parts = parts[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			resolved = filepath.Dir(resolved)
			continue
		}
		next := filepath.Join(resolved, part)
		info, err := os.Lstat(next)
		if os.IsNotExist(err) {
			resolved = next
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
			if len(parts) > 0 && !info.IsDir() {
				return "", fmt.Errorf("%q is not a directory", next)
			}
			if runtime.GOOS == "windows" {
				// Preserve EvalSymlinks' normalization of DOS short names and
				// actual filename case after resolving junctions ourselves.
				next, err = filepath.EvalSymlinks(next)
				if err != nil {
					return "", err
				}
			}
			resolved = next
			continue
		}
		links++
		if links > 255 {
			return "", errors.New("too many links resolving path")
		}
		link, err := os.Readlink(next)
		if err != nil {
			return "", err
		}
		if filepath.IsAbs(link) {
			volume = filepath.VolumeName(link)
			resolved = volume + string(filepath.Separator)
			link = link[len(volume):]
		} else if filepath.VolumeName(link) != "" || (len(link) > 0 && os.IsPathSeparator(link[0])) {
			return "", fmt.Errorf("ambiguous link destination %q", link)
		}
		parts = append(strings.Split(filepath.ToSlash(link), "/"), parts...)
	}
	return filepath.Clean(resolved), nil
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
