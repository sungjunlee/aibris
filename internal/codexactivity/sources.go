package codexactivity

import (
	"path/filepath"
	"slices"

	"github.com/sungjunlee/aibris/internal/codexhome"
)

// SourceCoverage speaks only for a home whose requested session roots were
// successfully queried. Other homes cannot supply negative activity evidence.
type SourceCoverage struct {
	Roots     []string `json:"roots"`
	Available bool     `json:"available"`
	// ActiveRoot reports that the home's sessions/ root exists as a directory.
	// An archive alone cannot vouch for the absence of recent sessions.
	ActiveRoot bool `json:"active_root"`
}

// LookupMember returns activity and whether every required source was queried.
// Codex-owned worktrees are home-scoped; Orca workspaces consult all homes.
// An available zero timestamp means that no matching session was found.
func (i Index) LookupMember(path string) (Worktree, bool) {
	if !i.Available || i.Err != nil {
		return Worktree{}, false
	}
	if id, project, ok := orcaWorkspaceFromCWD(path); ok {
		return i.lookupOrcaWorkspace(id, project)
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
	return i.memberActivity(selected, id, project), true
}

func (i Index) memberActivity(home, id, project string) Worktree {
	matching, ok := i.Members[MemberKey(home, id, project)]
	if !ok {
		matching = i.Worktrees[WorktreeKey(home, id)]
	}
	return matching
}

func (i Index) lookupOrcaWorkspace(id, project string) (Worktree, bool) {
	// An undiscovered Orca home cannot supply negative activity evidence,
	// even when another Codex home has readable sessions for this workspace.
	orca := codexhome.OrcaHome()
	if orca == "" || !i.Sources[canonicalPath(orca)].Available {
		return Worktree{}, false
	}
	homes, err := codexhome.Homes()
	if err != nil {
		return Worktree{}, false
	}
	combined := Worktree{WorktreeID: id, Project: project}
	for _, home := range canonicalRoots(homes) {
		coverage := i.Sources[home]
		roots := canonicalRoots([]string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")})
		// A session-root symlink can split this home's roots across source keys.
		// Partial coverage cannot speak for the whole home, even if it is readable.
		// Every home must also have its sessions/ root: a missing or dangling
		// one (for example on an unmounted volume) with only an archive present
		// would otherwise read as "no recent session".
		if !coverage.Available || !coverage.ActiveRoot || !sameRoots(coverage.Roots, roots) {
			return Worktree{}, false
		}
		matching := i.memberActivity(home, id, project)
		combined.SessionCount += matching.SessionCount
		if matching.LatestSession.After(combined.LatestSession) {
			combined.LatestSession = matching.LatestSession
		}
	}
	return combined, true
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
