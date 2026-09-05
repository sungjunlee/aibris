package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session-file discovery, record parsing, and CWD worktree identity for the
// Codex activity index. Index loading, cache I/O, and aggregation stay in
// codex_activity.go.

type codexSessionFileInfo struct {
	path    string
	modTime time.Time
	size    int64
}

func findCodexSessionFiles(ctx context.Context, roots []string) ([]codexSessionFileInfo, error) {
	seen := make(map[string]bool)
	var files []codexSessionFileInfo
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(root), ".jsonl") {
				files = appendSessionFileInfo(files, seen, root, info)
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			files = appendSessionFileInfo(files, seen, path, info)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})
	return files, nil
}

func appendSessionFileInfo(files []codexSessionFileInfo, seen map[string]bool, path string, info fs.FileInfo) []codexSessionFileInfo {
	cleanPath := filepath.Clean(path)
	if seen[cleanPath] {
		return files
	}
	seen[cleanPath] = true
	return append(files, codexSessionFileInfo{
		path:    cleanPath,
		modTime: info.ModTime(),
		size:    info.Size(),
	})
}

func readCodexSessionFileRecord(file codexSessionFileInfo) (codexActivityFileRecord, error) {
	record := codexActivityFileRecord{
		Path:    file.path,
		ModTime: file.modTime,
		Size:    file.size,
	}
	f, err := os.Open(file.path)
	if err != nil {
		return record, err
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return record, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return record, nil
	}

	var meta struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
			CWD       string `json:"cwd"`
			SessionID string `json:"session_id"`
			ID        string `json:"id"`
			ThreadID  string `json:"thread_id"`
		} `json:"payload"`
	}
	if err := json.Unmarshal([]byte(line), &meta); err != nil {
		return record, nil
	}
	if meta.Type != "session_meta" {
		return record, nil
	}
	sessionID := firstNonEmpty(meta.Payload.SessionID, meta.Payload.ID, meta.Payload.ThreadID)
	if sessionID == "" || meta.Timestamp == "" || meta.Payload.CWD == "" {
		return record, nil
	}
	timestamp, err := time.Parse(time.RFC3339Nano, meta.Timestamp)
	if err != nil {
		return record, nil
	}
	worktreeID, project, ok := codexActivityWorktreeFromCWD(meta.Payload.CWD)
	if !ok {
		return record, nil
	}

	record.Valid = true
	record.WorktreeID = worktreeID
	record.Project = project
	record.Timestamp = timestamp
	return record, nil
}

func codexActivityWorktreeFromCWD(cwd string) (string, string, bool) {
	parts := pathParts(cwd)
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] != ".codex" || !isCodexActivityWorktreeRoot(parts[i+1]) {
			continue
		}
		worktreeID := parts[i+2]
		project := worktreeID
		if i+3 < len(parts) {
			project = parts[i+3]
		}
		if worktreeID == "" || project == "" {
			return "", "", false
		}
		return worktreeID, project, true
	}
	return "", "", false
}

func pathParts(path string) []string {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	if volume != "" {
		clean = strings.TrimPrefix(clean, volume)
	}
	raw := strings.Split(clean, string(os.PathSeparator))
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

func isCodexActivityWorktreeRoot(name string) bool {
	return name == "worktree" ||
		name == "worktrees" ||
		strings.HasPrefix(name, "worktree-") ||
		strings.HasPrefix(name, "worktrees-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
