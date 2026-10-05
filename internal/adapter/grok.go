package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

// GrokSessionsAdapter reports Grok CLI session stores. Grok keeps one
// directory per working directory under ~/.grok/sessions, named with the
// URL-encoded absolute cwd, holding one subdirectory per session.
type GrokSessionsAdapter struct{}

func (a *GrokSessionsAdapter) Name() types.Tool {
	return types.ToolGrok
}

func (a *GrokSessionsAdapter) Category() types.Category {
	return types.CategoryAgentState
}

func (a *GrokSessionsAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}
	base, err := agentStateStoreRootFor(filepath.Join(".grok", "sessions"))
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		entryPath := filepath.Join(base, entry.Name())
		classification, reason, project, err := classifyRecordedCWDEntry(ctx, entryPath, recordedCWDFromGrokSessions)
		if err != nil {
			return nil, err
		}
		results = append(results, types.DebrisInfo{
			Tool:           types.ToolGrok,
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

func (a *GrokSessionsAdapter) RevalidateAgentState(ctx context.Context, entryPath string) (types.EntryClass, error) {
	classification, _, _, err := classifyRecordedCWDEntry(ctx, entryPath, recordedCWDFromGrokSessions)
	return classification, err
}

// maxGrokPromptContextBytes bounds one prompt_context.json read; real files
// are a few kilobytes.
const maxGrokPromptContextBytes = 1 << 20

// recordedCWDFromGrokSessions takes the cwd from two independent records: the
// entry name, which is the URL-encoded absolute cwd, and working_directory in
// every session's prompt_context.json. Absence can only be proven when every
// session agrees with the name; a disagreement, an unreadable session, or an
// entry with no session to corroborate the name leaves it undetermined.
func recordedCWDFromGrokSessions(ctx context.Context, entryPath string) (recordedCWDEvidence, error) {
	var evidence recordedCWDEvidence
	name := filepath.Base(entryPath)
	decoded, err := url.PathUnescape(name)
	if err != nil || !filepath.IsAbs(decoded) || filepath.Clean(decoded) != decoded {
		evidence.unverifiableRecords++
		evidence.firstUnverifiableRecord = "entry name is not an encoded absolute path"
		return evidence, nil
	}

	sessions, err := os.ReadDir(entryPath)
	if err != nil {
		evidence.unverifiableFiles = append(evidence.unverifiableFiles, name)
		return evidence, nil //nolint:nilerr // unreadable entry is undetermined, not a scan failure
	}
	corroborated := 0
	for _, session := range sessions {
		if err := ctx.Err(); err != nil {
			return recordedCWDEvidence{}, err
		}
		if !session.IsDir() {
			continue
		}
		contextFile := filepath.Join(entryPath, session.Name(), "prompt_context.json")
		cwd, err := grokPromptContextCWD(contextFile)
		switch {
		case errors.Is(err, os.ErrNotExist):
			// A session that never wrote its context records nothing either way.
			continue
		case err != nil:
			evidence.unverifiableFiles = append(evidence.unverifiableFiles,
				filepath.Join(session.Name(), "prompt_context.json"))
			continue
		}
		if filepath.Clean(cwd) != decoded {
			evidence.unverifiableRecords++
			if evidence.firstUnverifiableRecord == "" {
				evidence.firstUnverifiableRecord = session.Name() + ": working_directory differs from the entry name"
			}
			continue
		}
		corroborated++
	}
	if corroborated == 0 {
		evidence.unverifiableRecords++
		if evidence.firstUnverifiableRecord == "" {
			evidence.firstUnverifiableRecord = "no session records a working_directory"
		}
		return evidence, nil
	}
	evidence.cwds = []string{decoded}
	return evidence, nil
}

func grokPromptContextCWD(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxGrokPromptContextBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxGrokPromptContextBytes {
		return "", errors.New("prompt_context.json too large")
	}
	var context struct {
		WorkingDirectory string `json:"working_directory"`
	}
	if err := json.Unmarshal(data, &context); err != nil {
		return "", err
	}
	if context.WorkingDirectory == "" || !filepath.IsAbs(context.WorkingDirectory) {
		return "", errors.New("working_directory missing or not absolute")
	}
	return context.WorkingDirectory, nil
}
