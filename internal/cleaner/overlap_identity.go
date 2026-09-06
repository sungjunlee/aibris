package cleaner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type canonicalPathIdentity struct {
	raw       string
	canonical string
	info      os.FileInfo
}

func canonicalExistingPathIdentity(path string) (canonicalPathIdentity, error) {
	raw := strings.TrimSpace(path)
	if raw == "" || !filepath.IsAbs(raw) {
		return canonicalPathIdentity{}, fmt.Errorf("path is not absolute")
	}
	raw = filepath.Clean(raw)
	canonical, err := filepath.EvalSymlinks(raw)
	if err != nil {
		return canonicalPathIdentity{}, fmt.Errorf("resolving %q: %w", raw, err)
	}
	canonical = filepath.Clean(canonical)
	info, err := os.Stat(canonical)
	if err != nil {
		return canonicalPathIdentity{}, fmt.Errorf("reading %q: %w", canonical, err)
	}
	return canonicalPathIdentity{raw: raw, canonical: canonical, info: info}, nil
}

func (identity canonicalPathIdentity) unchanged() error {
	current, err := canonicalExistingPathIdentity(identity.raw)
	if err != nil {
		return err
	}
	return identity.matches(current)
}

func (identity canonicalPathIdentity) matches(current canonicalPathIdentity) error {
	if identity.canonical != current.canonical {
		return fmt.Errorf("canonical path changed from %q to %q", identity.canonical, current.canonical)
	}
	if identity.info == nil || current.info == nil || !os.SameFile(identity.info, current.info) {
		return fmt.Errorf("filesystem identity changed at %q", identity.canonical)
	}
	return nil
}

func canonicalOverlapRelation(target, agentState string) (OverlapSafetyRelation, bool) {
	if target == agentState {
		return OverlapRelationExact, true
	}
	if PathContains(agentState, target) {
		return OverlapRelationAgentStateAncestor, true
	}
	if PathContains(target, agentState) {
		return OverlapRelationAgentStateDescendant, true
	}
	return "", false
}

func ambiguousAgentStateMayOverlap(
	target canonicalPathIdentity,
	agentStatePath string,
) (bool, error) {
	rawAgentState := filepath.Clean(strings.TrimSpace(agentStatePath))
	if !filepath.IsAbs(rawAgentState) {
		return false, nil
	}

	for _, targetPath := range []string{target.raw, target.canonical} {
		if _, overlaps := canonicalOverlapRelation(targetPath, rawAgentState); overlaps {
			return true, nil
		}
	}

	resolved, err := resolvePathWithUnresolvedSuffix(rawAgentState, 0)
	if err != nil {
		return false, fmt.Errorf("resolving %q: %w", rawAgentState, err)
	}
	if resolved == rawAgentState {
		return false, nil
	}
	for _, targetPath := range []string{target.raw, target.canonical} {
		if _, overlaps := canonicalOverlapRelation(targetPath, resolved); overlaps {
			return true, nil
		}
	}
	return false, nil
}

func resolvePathWithUnresolvedSuffix(path string, symlinkDepth int) (string, error) {
	if symlinkDepth > 255 {
		return "", fmt.Errorf("too many symlinks resolving %q", path)
	}

	current := filepath.Clean(path)
	var suffix []string
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				linkTarget, err := os.Readlink(current)
				if err != nil {
					return "", err
				}
				if !filepath.IsAbs(linkTarget) {
					linkTarget = filepath.Join(filepath.Dir(current), linkTarget)
				}
				current, err = resolvePathWithUnresolvedSuffix(linkTarget, symlinkDepth+1)
				if err != nil {
					return "", err
				}
			} else {
				current, err = filepath.EvalSymlinks(current)
				if err != nil {
					return "", err
				}
			}
			for _, part := range suffix {
				current = filepath.Join(current, part)
			}
			return filepath.Clean(current), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append([]string{filepath.Base(current)}, suffix...)
		current = parent
	}
}
