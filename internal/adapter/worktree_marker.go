package adapter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

// This file is the marker-inspection cluster for worktree units: a readable
// "gitdir:" pointer maps to active/orphaned, a missing, empty, malformed, or
// directory marker maps to a review-only plain-dir reason, and I/O failures
// surface as provider errors (partial scan). It never classifies a unit as a
// clean candidate; aggregation and cleanup policy stay in scanEntry.

type worktreeMarkerState int

const (
	worktreeMarkerMissing worktreeMarkerState = iota
	worktreeMarkerValid
	worktreeMarkerInvalid
)

const worktreeMarkerDirectoryReason = ".git marker is a directory"

type worktreeMarkerInspection struct {
	state  worktreeMarkerState
	status types.WorktreeStatus
	reason string
}

func inspectWorktreeMarker(ctx context.Context, gitFilePath string) (worktreeMarkerInspection, error) {
	if err := ctx.Err(); err != nil {
		return worktreeMarkerInspection{}, err
	}

	info, err := os.Lstat(gitFilePath)
	if os.IsNotExist(err) {
		return worktreeMarkerInspection{state: worktreeMarkerMissing}, nil
	}
	if err != nil {
		return worktreeMarkerInspection{}, fmt.Errorf("inspecting worktree marker %q: %w", gitFilePath, err)
	}
	if info.IsDir() {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: worktreeMarkerDirectoryReason,
		}, nil
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is not a regular file",
		}, nil
	}

	f, err := os.Open(gitFilePath)
	if err != nil {
		return worktreeMarkerInspection{}, fmt.Errorf("reading worktree marker %q: %w", gitFilePath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return worktreeMarkerInspection{}, fmt.Errorf("reading worktree marker %q: %w", gitFilePath, err)
		}
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is empty",
		}, nil
	}
	line := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(line, "gitdir: ") {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is malformed",
		}, nil
	}
	gitdirPath := strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	if gitdirPath == "" {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is malformed",
		}, nil
	}
	if !filepath.IsAbs(gitdirPath) {
		gitdirPath = filepath.Join(filepath.Dir(gitFilePath), gitdirPath)
	}
	if _, err := os.Stat(gitdirPath); os.IsNotExist(err) {
		return worktreeMarkerInspection{
			state:  worktreeMarkerValid,
			status: types.WorktreeOrphaned,
		}, nil
	} else if err != nil {
		return worktreeMarkerInspection{}, fmt.Errorf("validating gitdir %q from %q: %w", gitdirPath, gitFilePath, err)
	}
	return worktreeMarkerInspection{
		state:  worktreeMarkerValid,
		status: types.WorktreeActive,
	}, nil
}

func isLinkedWorktreeOwner(ctx context.Context, path string, memberDepth int) (bool, error) {
	if present, err := hasLinkedWorktreeMarker(ctx, path); present || err != nil {
		return present, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("reading worktree unit %q: %w", path, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(path, entry.Name())
		if present, err := hasLinkedDescendantMarker(ctx, child, memberDepth); present || err != nil {
			return present, err
		}
	}
	return false, nil
}

func hasLinkedDescendantMarker(ctx context.Context, path string, memberDepth int) (bool, error) {
	if present, err := hasLinkedWorktreeMarker(ctx, path); present || err != nil {
		return present, err
	}
	if memberDepth < registeredWorktreeMemberDepth {
		return false, nil
	}
	return hasLinkedChildMarker(ctx, path)
}

func hasLinkedChildMarker(ctx context.Context, path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("reading worktree leaf %q: %w", path, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if present, err := hasLinkedWorktreeMarker(ctx, filepath.Join(path, entry.Name())); present || err != nil {
			return present, err
		}
	}
	return false, nil
}

func hasLinkedWorktreeMarker(ctx context.Context, path string) (bool, error) {
	inspection, err := inspectWorktreeMarker(ctx, filepath.Join(path, ".git"))
	if err != nil {
		return false, err
	}
	if inspection.state == worktreeMarkerValid {
		return true, nil
	}
	return inspection.state == worktreeMarkerInvalid && inspection.reason != worktreeMarkerDirectoryReason, nil
}
