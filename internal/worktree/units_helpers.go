package worktree

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
)

// discoverGitWorktreeMemberPaths finds the linked checkouts that make up one
// cleanup unit from filesystem structure alone; it runs no Git commands.
// Membership never depends on Git evidence, so counting units needs only
// this, and evidence can then be gathered for all members concurrently.
func discoverGitWorktreeMemberPaths(ctx context.Context, targetPath string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	linked, invalidPresent, err := ownerGitMarkerState(targetPath)
	if err != nil {
		return nil, err
	}
	if invalidPresent {
		return nil, nil
	}
	if linked {
		return []string{targetPath}, nil
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, err
	}

	memberPaths := make(map[string]bool)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || adapter.IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		memberPath := filepath.Join(targetPath, entry.Name())
		if HasGitWorktreeMetadata(memberPath) {
			if canonicalPath, ok := cleaner.TargetPathKey(memberPath); ok {
				memberPaths[canonicalPath] = true
			}
			continue
		}
		keep, nested, err := classifyMissingCleanupMember(ctx, memberPath)
		if err != nil {
			return nil, err
		}
		if !keep {
			return nil, nil
		}
		for _, nestedPath := range nested {
			memberPaths[nestedPath] = true
		}
	}

	paths := make([]string, 0, len(memberPaths))
	for path := range memberPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

// gitEvidenceWorkers bounds concurrent Git evidence collection. Each member
// runs a handful of short git processes; more workers than this mostly
// contend for the same disk.
const gitEvidenceWorkers = 8

// buildGitWorktreeMembers gathers Git evidence for each path concurrently and
// returns members in the order of paths.
func buildGitWorktreeMembers(ctx context.Context, paths []string) []GitWorktreeMember {
	members := make([]GitWorktreeMember, len(paths))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(gitEvidenceWorkers, len(paths)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				members[i] = BuildGitWorktreeMember(ctx, paths[i])
			}
		}()
	}
	for i := range paths {
		if ctx.Err() != nil {
			break
		}
		next <- i
	}
	close(next)
	wg.Wait()
	return members
}

func classifyMissingCleanupMember(ctx context.Context, memberPath string) (bool, []string, error) {
	empty, hasSubdirs, err := adapter.LeftoverMemberState(memberPath)
	if err != nil {
		return false, nil, err
	}
	if empty {
		return true, nil, nil
	}
	if !hasSubdirs {
		return false, nil, nil
	}
	nested, mixed, err := twoLevelGitWorktreePaths(ctx, memberPath)
	if err != nil {
		return false, nil, err
	}
	if mixed || len(nested) == 0 {
		return false, nil, nil
	}
	return true, nested, nil
}

func twoLevelGitWorktreePaths(ctx context.Context, leafPath string) ([]string, bool, error) {
	entries, err := os.ReadDir(leafPath)
	if err != nil {
		return nil, false, err
	}
	var paths []string
	missing := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if !entry.IsDir() || adapter.IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		checkout := filepath.Join(leafPath, entry.Name())
		if !HasGitWorktreeMetadata(checkout) {
			missing = true
			continue
		}
		canonical, ok := cleaner.TargetPathKey(checkout)
		if ok {
			paths = append(paths, canonical)
		}
	}
	if missing {
		return nil, true, nil
	}
	sort.Strings(paths)
	return paths, false, nil
}

func ownerGitMarkerState(path string) (linked bool, invalidPresent bool, err error) {
	_, err = os.Lstat(filepath.Join(path, ".git"))
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if HasGitWorktreeMetadata(path) {
		return true, false, nil
	}
	return false, true, nil
}
