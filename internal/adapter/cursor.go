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

	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	base, err := agentStateStoreRootFor(filepath.Join(".cursor", "projects"))
	if err != nil {
		return nil, err
	}
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
		results = append(results, types.DebrisInfo{
			Tool:           types.ToolCursor,
			Category:       types.CategoryAgentState,
			ID:             entry.Name(),
			Project:        project,
			Path:           entryPath,
			ModTime:        agentStoreActivityModTime(ctx, entryPath, info.ModTime()),
			PathModTime:    info.ModTime(),
			Classification: classification,
			Reason:         reason,
		})
	}
	sizePaths := make([]string, 0, len(results))
	for _, result := range results {
		sizePaths = append(sizePaths, result.Path)
	}
	sizes := estimateDirSizes(ctx, sizePaths)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Size = sizes[results[i].Path]
	}
	return results, nil
}

// ClassifyCursorProjectEntry re-derives the cleanup-driving classification
// from the current contents of a Cursor project-store entry.
func ClassifyCursorProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, error) {
	classification, _, _, err := classifyCursorProjectEntry(ctx, entryPath)
	return classification, err
}

func (a *CursorAdapter) RevalidateAgentState(ctx context.Context, entryPath string) (types.EntryClass, error) {
	return ClassifyCursorProjectEntry(ctx, entryPath)
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
	cwds, unterminatedWorkspacePath, err := cursorWorkspacePaths(ctx, workerLog, filepath.Join(home, ".cursor"))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return recordedCWDEvidence{}, err
		}
		evidence.unverifiableFiles = append(evidence.unverifiableFiles, filepath.Base(workerLog))
		return evidence, nil
	}
	if unterminatedWorkspacePath {
		evidence.unverifiableRecords++
		evidence.firstUnverifiableRecord = filepath.Base(workerLog) + ": unterminated workspacePath record"
	}
	for _, cwd := range cwds {
		evidence.cwds = append(evidence.cwds, cwd)
		if strings.IndexFunc(cwd, unicode.IsSpace) >= 0 {
			evidence.unverifiableRecords++
			if evidence.firstUnverifiableRecord == "" {
				evidence.firstUnverifiableRecord = filepath.Base(workerLog) + ": ambiguous workspacePath=" + cwd
			}
		}
	}
	return evidence, nil
}

func cursorWorkspacePaths(ctx context.Context, workerLog, cursorRoot string) ([]string, bool, error) {
	file, err := os.Open(workerLog)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()

	var paths []string
	var unterminatedWorkspacePath bool
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), maxCursorWorkerLogLineBytes)
	var tokenUnterminated bool
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		advance, token, splitErr := bufio.ScanLines(data, atEOF)
		if token != nil {
			tokenUnterminated = atEOF && len(data) > 0 && advance == len(data)
		}
		return advance, token, splitErr
	})
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		line := scanner.Text()
		index := strings.Index(line, "workspacePath=")
		if index < 0 {
			continue
		}
		if tokenUnterminated {
			unterminatedWorkspacePath = true
			continue
		}
		value := strings.TrimSpace(line[index+len("workspacePath="):])
		if filepath.IsAbs(value) && !cursorWorkspaceUnderStore(value, cursorRoot) {
			value = filepath.Clean(value)
			if !seen[value] {
				seen[value] = true
				paths = append(paths, value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	return paths, unterminatedWorkspacePath, nil
}

func cursorWorkspaceUnderStore(path, cursorRoot string) bool {
	path = filepath.Clean(path)
	cursorRoot = filepath.Clean(cursorRoot)
	return path == cursorRoot ||
		strings.HasPrefix(path, cursorRoot+string(filepath.Separator)) ||
		pathWithinContainer(path, cursorRoot)
}
