// Package exclude resolves user exclusion patterns against approved scan
// roots. Exclusions only remove paths from discovery results; they never
// broaden deletion authority.
package exclude

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

// Pattern is one raw exclusion pattern and the source it came from.
type Pattern struct {
	Raw    string
	Source types.ExcludeSource
}

// Matcher matches discovered debris paths against exclusion scopes that
// resolved inside the approved scan roots.
type Matcher struct {
	scopes   []*types.ExcludedScope
	rejected []types.RejectedExclude
}

// New resolves patterns against roots. A pattern is only honored when it
// resolves to a canonical path inside an approved scan root; anything else is
// recorded as rejected. New only decides exclusion scope; it never touches
// debris content.
func New(patterns []Pattern, roots []string) *Matcher {
	m := &Matcher{}
	home, _ := os.UserHomeDir()
	for _, pattern := range patterns {
		raw := strings.TrimSpace(pattern.Raw)
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		m.add(raw, pattern.Source, home, roots)
	}
	return m
}

func (m *Matcher) add(raw string, source types.ExcludeSource, home string, roots []string) {
	// A pattern carrying a `..` traversal segment is rejected outright: it can
	// silently escape a scan root after cleaning, so we never honor it as an
	// exclusion scope. This is part of the path-traversal safety contract.
	if containsDotDotSegment(raw) {
		m.reject(raw, source, "outside scan roots")
		return
	}
	added := false
	for _, candidate := range expandPattern(raw, home, roots) {
		if strings.ContainsAny(candidate, "*?[") {
			matches, err := filepath.Glob(candidate)
			if err != nil {
				m.reject(raw, source, "invalid glob pattern")
				return
			}
			for _, match := range matches {
				if resolved, ok := scopeWithinRoots(match, roots); ok {
					m.addScope(raw, resolved, source)
					added = true
				}
			}
			continue
		}
		if resolved, ok := scopeWithinRoots(candidate, roots); ok {
			m.addScope(raw, resolved, source)
			added = true
		}
	}
	if !added {
		m.reject(raw, source, "outside scan roots")
	}
}

// containsDotDotSegment reports whether raw contains a `..` path segment.
// It inspects the RAW pattern before any lexical cleaning, because a leading
// `..` or embedded `..` segment is a traversal marker that cleaning would
// silently collapse; we reject such patterns outright so they cannot be
// re-scoped to escape a scan root.
func containsDotDotSegment(raw string) bool {
	for _, seg := range strings.Split(raw, string(filepath.Separator)) {
		if seg == ".." {
			return true
		}
	}
	return false
}

func (m *Matcher) addScope(pattern, resolved string, source types.ExcludeSource) {
	m.scopes = append(m.scopes, &types.ExcludedScope{
		Pattern:  pattern,
		Resolved: resolved,
		Source:   source,
	})
}

func (m *Matcher) reject(pattern string, source types.ExcludeSource, reason string) {
	m.rejected = append(m.rejected, types.RejectedExclude{
		Pattern: pattern,
		Source:  source,
		Reason:  reason,
	})
}

// expandPattern returns candidate absolute patterns for raw. `~` expands to
// the home directory, absolute paths are kept as-is, and relative patterns
// are anchored at every scan root so they can never point outside the roots.
func expandPattern(raw, home string, roots []string) []string {
	if raw == "~" {
		if home == "" {
			return nil
		}
		return []string{home}
	}
	if strings.HasPrefix(raw, "~/") {
		if home == "" {
			return nil
		}
		return []string{filepath.Join(home, strings.TrimPrefix(raw, "~/"))}
	}
	if filepath.IsAbs(raw) {
		return []string{raw}
	}
	candidates := make([]string, 0, len(roots))
	for _, root := range roots {
		candidates = append(candidates, filepath.Join(root, raw))
	}
	return candidates
}

// scopeWithinRoots canonicalizes path and returns the canonical form only if
// it stays inside an approved scan root. Symlinks are resolved so a link
// pointing outside the roots cannot exclude outside content, and
// filepath.Clean collapses `..` traversal before the containment check.
func scopeWithinRoots(path string, roots []string) (string, bool) {
	canonical := canonicalize(path)
	for _, root := range roots {
		if canonical == root || adapter.IsWithin(root, canonical) {
			return canonical, true
		}
	}
	return "", false
}

func canonicalize(path string) string {
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return filepath.Clean(resolved)
	}
	// The path does not exist. Walk up to the deepest existing ancestor,
	// accumulating the missing suffix segments in order, then re-append them
	// under the resolved ancestor so a nonexistent path under a symlinked
	// prefix still canonicalizes against resolved scan roots without dropping
	// any intermediate segment.
	var missing []string
	dir := clean
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		missing = append([]string{filepath.Base(dir)}, missing...)
		dir = parent
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			full := resolved
			for _, seg := range missing {
				full = filepath.Join(full, seg)
			}
			return filepath.Clean(full)
		}
	}
	return clean
}

// Match reports whether path is covered by one honored exclusion scope and
// records the match for diagnostics.
func (m *Matcher) Match(path string) bool {
	if len(m.scopes) == 0 {
		return false
	}
	clean := filepath.Clean(path)
	// canonicalize resolves symlinks — including the deepest existing ancestor
	// when the candidate itself does not exist (e.g. a non-existent descendant
	// under a symlinked prefix). This keeps candidate comparison consistent
	// with the canonical (resolved) scope paths, matching on macOS/Linux where
	// /tmp, /var/folders, etc. are symlinks.
	candidates := []string{clean}
	if canon := canonicalize(clean); canon != clean {
		candidates = append(candidates, canon)
	}
	for _, candidate := range candidates {
		for _, scope := range m.scopes {
			if candidate == scope.Resolved || adapter.IsWithin(scope.Resolved, candidate) {
				scope.Count++
				return true
			}
		}
	}
	return false
}

// Scopes returns the honored exclusion scopes with their match counts.
func (m *Matcher) Scopes() []types.ExcludedScope {
	scopes := make([]types.ExcludedScope, 0, len(m.scopes))
	for _, scope := range m.scopes {
		scopes = append(scopes, *scope)
	}
	return scopes
}

// Rejected returns the exclusion patterns that were not honored.
func (m *Matcher) Rejected() []types.RejectedExclude {
	return append([]types.RejectedExclude(nil), m.rejected...)
}

// UserIgnoreFile returns the documented per-user ignore file location:
// $XDG_CONFIG_HOME/aibris/ignore, falling back to ~/.config/aibris/ignore.
// It reads XDG_CONFIG_HOME explicitly rather than via os.UserConfigDir, which
// on macOS ignores XDG_CONFIG_HOME and returns $HOME/Library/Application
// Support — diverging from the documented Linux/XDG path the ignore file is
// written to.
func UserIgnoreFile() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "aibris", "ignore")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "aibris", "ignore")
}

// RootIgnoreFile returns the repo-local ignore file location directly under
// root.
func RootIgnoreFile(root string) string {
	return filepath.Join(root, ".aibris-ignore")
}

// IgnoreFilePatterns reads persistent exclusion patterns from the per-user
// ignore file and from a .aibris-ignore file directly under each scan root.
// Each non-empty, non-comment line is one pattern; missing files contribute
// nothing.
func IgnoreFilePatterns(roots []string) []Pattern {
	files := []string{UserIgnoreFile()}
	for _, root := range roots {
		files = append(files, RootIgnoreFile(root))
	}
	var patterns []Pattern
	for _, file := range files {
		for _, raw := range readIgnoreFile(file) {
			patterns = append(patterns, Pattern{Raw: raw, Source: types.ExcludeSourceIgnoreFile})
		}
	}
	return patterns
}

func readIgnoreFile(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
