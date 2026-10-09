package codexactivity

import (
	"errors"
	"io"
	"os"
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
	// Absent is proven absence of the unconfigured default home or both stores.
	// It supplies zero activity only to Orca's aggregation, never native worktrees.
	Absent bool `json:"absent"`
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
	if selected == "" || !i.homeAvailable(selected, false) {
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
		if !i.homeAvailable(home, true) {
			return Worktree{}, false
		}
		if i.Sources[home].Absent {
			continue
		}
		matching := i.memberActivity(home, id, project)
		combined.SessionCount += matching.SessionCount
		if matching.LatestSession.After(combined.LatestSession) {
			combined.LatestSession = matching.LatestSession
		}
	}
	return combined, true
}

// homeAvailable decides whether a home can supply negative activity evidence.
// Native lookups may query only sessions; Orca requires both requested roots.
// Only Orca aggregation can accept a still-provably-absent default home.
func (i Index) homeAvailable(home string, requireAllRoots bool) bool {
	coverage := i.Sources[home]
	if requireAllRoots && coverage.Absent {
		// Cached absence must not authorize a now-configured or populated home.
		return defaultHomeAbsent(home)
	}
	if !coverage.Available || !coverage.ActiveRoot || len(coverage.Roots) == 0 {
		return false
	}
	if !homeStoresAvailable(home) {
		return false
	}
	roots := canonicalRoots([]string{filepath.Join(home, "sessions"), filepath.Join(home, "archived_sessions")})
	// Check both indexed and current roots: a symlink may split the home into
	// different source keys, or change after the index was cached.
	for _, root := range append(roots, coverage.Roots...) {
		if filepath.Dir(root) != home {
			return false
		}
	}
	return !requireAllRoots || sameRoots(coverage.Roots, roots)
}

// homeStoresAvailable checks both store leaves without treating a dangling
// symlink or a Windows mount-point reparse point as a missing optional archive.
// Run at coverage time and lookup so cached roots cannot bypass live evidence.
func homeStoresAvailable(home string) bool {
	for _, store := range []string{"sessions", "archived_sessions"} {
		path := filepath.Join(home, store)
		info, err := os.Lstat(path)
		if err != nil {
			if store == "archived_sessions" && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false
		}
		if info.Mode()&os.ModeIrregular != 0 {
			return false
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Stat(path)
			if err != nil || !target.IsDir() || target.Mode()&os.ModeIrregular != 0 {
				return false
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil || filepath.Dir(resolved) != home {
				return false
			}
		} else if !info.IsDir() {
			return false
		}
		// Metadata survives a permission change; cached negative evidence
		// needs a store that can still be listed now.
		if !directoryReadable(path) {
			return false
		}
	}
	return true
}

func directoryReadable(path string) bool {
	dir, err := os.Open(path)
	if err != nil {
		return false
	}
	defer dir.Close()
	names, err := dir.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err != nil || len(names) == 0 {
		return false
	}
	// Listing needs read permission; reaching an entry also needs search.
	_, err = os.Lstat(filepath.Join(path, names[0]))
	return err == nil
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
