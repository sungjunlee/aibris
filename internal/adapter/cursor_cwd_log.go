package adapter

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const maxCursorWorkerLogLineBytes = 1024 * 1024

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
