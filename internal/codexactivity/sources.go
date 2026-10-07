package codexactivity

import (
	"path/filepath"
	"slices"
)

// SourceCoverage speaks only for a home whose requested session roots were
// successfully queried. Other homes cannot supply negative activity evidence.
type SourceCoverage struct {
	Roots     []string `json:"roots"`
	Available bool     `json:"available"`
}

// LookupMember returns home-scoped activity and whether the source was queried.
// An available zero timestamp means that no matching session was found.
func (i Index) LookupMember(path string) (Worktree, bool) {
	if !i.Available || i.Err != nil {
		return Worktree{}, false
	}
	// Prefer the innermost home when configured stores are nested.
	var selected, id, project string
	for home := range i.Sources {
		worktreeID, name, ok := WorktreeFromCWD(path, home)
		if ok && len(home) > len(selected) {
			selected, id, project = home, worktreeID, name
		}
	}
	if selected == "" || !i.Sources[selected].Available {
		return Worktree{}, false
	}
	matching, ok := i.Members[MemberKey(selected, id, project)]
	if !ok {
		matching = i.Worktrees[WorktreeKey(selected, id)]
	}
	return matching, true
}

func sameRoots(a, b []string) bool { return slices.Equal(a, b) }

func canonicalRoots(roots []string) []string {
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		result = append(result, canonicalPath(root))
	}
	slices.Sort(result)
	return slices.Compact(result)
}

// Resolve existing ancestors too: CWDs and session roots may no longer exist,
// while a configured home or a platform temp directory is a symlink.
func canonicalPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	ancestor := absolute
	var suffix []string
	for {
		if resolved, err := filepath.EvalSymlinks(ancestor); err == nil {
			for j := len(suffix) - 1; j >= 0; j-- {
				resolved = filepath.Join(resolved, suffix[j])
			}
			return resolved
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return absolute
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
}
