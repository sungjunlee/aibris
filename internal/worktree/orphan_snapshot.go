package worktree

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/pathidentity"
)

// ErrWorktreeEvidenceChanged identifies an orphan cleanup refusal without
// putting paths or Git metadata in machine-readable receipt reason codes.
var ErrWorktreeEvidenceChanged = errors.New("worktree evidence changed; rescan and review cleanup")

// OrphanedWorktreeSnapshot captures member and marker evidence before
// confirmation. The scan status is a claim, never deletion authority.
type OrphanedWorktreeSnapshot struct {
	path    string
	members []orphanedMemberSnapshot
}

type orphanedMemberSnapshot struct {
	path           string
	identity       string
	markerIdentity string
	markerInfo     os.FileInfo
	markerDigest   [sha256.Size]byte
}

// CaptureOrphanedWorktreeSnapshot refuses any owner that current Git evidence
// cannot justify as entirely orphaned. It runs no Git commands.
func CaptureOrphanedWorktreeSnapshot(ctx context.Context, path string) (*OrphanedWorktreeSnapshot, error) {
	snapshot, err := captureOrphanedWorktreeSnapshot(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWorktreeEvidenceChanged, err)
	}
	return snapshot, nil
}

func captureOrphanedWorktreeSnapshot(ctx context.Context, path string) (*OrphanedWorktreeSnapshot, error) {
	if err := adapter.RequireOrphanedWorktreeOwner(ctx, path); err != nil {
		return nil, err
	}
	paths, err := discoverGitWorktreeMemberPaths(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("enumerating current worktree members: %w", err)
	}
	if len(paths) == 0 {
		return nil, errors.New("current worktree member evidence unavailable")
	}
	snapshot := &OrphanedWorktreeSnapshot{path: path}
	for _, memberPath := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, identity, err := pathidentity.PathIdentity(memberPath)
		if err != nil {
			return nil, fmt.Errorf("inspecting worktree member %q: %w", memberPath, err)
		}
		markerPath := filepath.Join(memberPath, ".git")
		info, markerIdentity, err := pathidentity.PathIdentity(markerPath)
		if err != nil {
			return nil, fmt.Errorf("inspecting worktree marker %q: %w", markerPath, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("worktree marker %q is not a regular file", markerPath)
		}
		contents, err := os.ReadFile(markerPath)
		if err != nil {
			return nil, fmt.Errorf("reading worktree marker %q: %w", markerPath, err)
		}
		snapshot.members = append(snapshot.members, orphanedMemberSnapshot{
			path: memberPath, identity: identity, markerIdentity: markerIdentity,
			markerInfo: info, markerDigest: sha256.Sum256(contents),
		})
	}
	// Reclassify after capturing markers, including external gitdir evidence.
	if err := adapter.RequireOrphanedWorktreeOwner(ctx, path); err != nil {
		return nil, err
	}
	return snapshot, nil
}

// Validate re-derives orphan authority at the mutation boundary and refuses
// changes to the approved members or markers even if all still look orphaned.
func (s *OrphanedWorktreeSnapshot) Validate(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("%w: orphaned worktree snapshot unavailable", ErrWorktreeEvidenceChanged)
	}
	current, err := CaptureOrphanedWorktreeSnapshot(ctx, s.path)
	if err != nil {
		return err
	}
	if len(current.members) != len(s.members) {
		return fmt.Errorf("%w: worktree member set changed for %q", ErrWorktreeEvidenceChanged, s.path)
	}
	for i, member := range s.members {
		fresh := current.members[i]
		if member.path != fresh.path || member.identity != fresh.identity {
			return fmt.Errorf("%w: worktree member set changed for %q", ErrWorktreeEvidenceChanged, s.path)
		}
		if member.markerIdentity != fresh.markerIdentity ||
			!os.SameFile(member.markerInfo, fresh.markerInfo) ||
			!member.markerInfo.ModTime().Equal(fresh.markerInfo.ModTime()) ||
			member.markerDigest != fresh.markerDigest {
			return fmt.Errorf("%w: worktree marker changed for %q", ErrWorktreeEvidenceChanged, member.path)
		}
	}
	return nil
}
