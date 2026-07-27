package adapter

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sungjunlee/aibris/internal/types"
)

const maxCursorWorkerLogLineBytes = 1024 * 1024

type CursorAdapter struct{}

func (a *CursorAdapter) Name() types.Tool {
	return types.ToolCursor
}

func (a *CursorAdapter) Category() types.Category {
	return types.CategoryAgentState
}

func (a *CursorAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	base := filepath.Join(home, ".cursor", "projects")
	if !pathUnderRoots(base, roots) {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []types.DebrisInfo
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath := filepath.Join(base, entry.Name())
		classification, reason, project, err := classifyCursorProjectEntry(ctx, entryPath)
		if err != nil {
			return nil, err
		}
		size := estimateDirSize(ctx, entryPath)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, types.DebrisInfo{
			Tool:           types.ToolCursor,
			Category:       types.CategoryAgentState,
			ID:             entry.Name(),
			Project:        project,
			Path:           entryPath,
			Size:           size,
			ModTime:        info.ModTime(),
			Classification: classification,
			Reason:         reason,
		})
	}
	return results, nil
}

// ClassifyCursorProjectEntry re-derives the cleanup-driving classification
// from the current contents of a Cursor project-store entry.
func ClassifyCursorProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, error) {
	classification, _, _, err := classifyCursorProjectEntry(ctx, entryPath)
	return classification, err
}

func classifyCursorProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, string, string, error) {
	return classifyRecordedCWDEntry(ctx, entryPath, recordedCWDFromCursorProject)
}

func recordedCWDFromCursorProject(ctx context.Context, entryPath string) (recordedCWDEvidence, error) {
	var evidence recordedCWDEvidence
	home, err := os.UserHomeDir()
	if err != nil {
		return evidence, err
	}
	workerLog := filepath.Join(entryPath, "worker.log")
	cwd, err := firstCursorWorkspacePath(ctx, workerLog, filepath.Join(home, ".cursor"))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return recordedCWDEvidence{}, err
		}
		evidence.unverifiableFiles = append(evidence.unverifiableFiles, filepath.Base(workerLog))
		return evidence, nil
	}
	if cwd != "" {
		evidence.cwds = append(evidence.cwds, cwd)
		if strings.IndexFunc(cwd, unicode.IsSpace) >= 0 {
			evidence.unverifiableRecords++
			evidence.firstUnverifiableRecord = filepath.Base(workerLog) + ": ambiguous workspacePath=" + cwd
		}
	}
	return evidence, nil
}

func firstCursorWorkspacePath(ctx context.Context, workerLog, cursorRoot string) (string, error) {
	file, err := os.Open(workerLog)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxCursorWorkerLogLineBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := scanner.Text()
		index := strings.Index(line, "workspacePath=")
		if index < 0 {
			continue
		}
		value := strings.TrimSpace(line[index+len("workspacePath="):])
		if filepath.IsAbs(value) && !cursorWorkspaceUnderStore(value, cursorRoot) {
			return filepath.Clean(value), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func cursorWorkspaceUnderStore(path, cursorRoot string) bool {
	path = filepath.Clean(path)
	cursorRoot = filepath.Clean(cursorRoot)
	return path == cursorRoot ||
		strings.HasPrefix(path, cursorRoot+string(filepath.Separator)) ||
		pathWithinContainer(path, cursorRoot)
}
