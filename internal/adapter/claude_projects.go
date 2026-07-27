package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

const maxRecordedCWDBytes = 64 * 1024

// ClaudeProjectAdapter discovers Claude Code project-store entries and
// classifies them from the working directory recorded in session metadata.
type ClaudeProjectAdapter struct{}

func (a *ClaudeProjectAdapter) Name() types.Tool {
	return types.ToolClaude
}

func (a *ClaudeProjectAdapter) Category() types.Category {
	return types.CategoryAgentState
}

func (a *ClaudeProjectAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	base := filepath.Join(home, ".claude", "projects")
	if !pathUnderRoots(base, roots) {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
		classification, reason, project, err := classifyClaudeProjectEntry(ctx, entryPath)
		if err != nil {
			return nil, err
		}
		size := estimateDirSize(ctx, entryPath)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, types.DebrisInfo{
			Tool:           types.ToolClaude,
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

type recordedCWDEvidence struct {
	cwds              []string
	unverifiableFiles []string
}

func classifyClaudeProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, string, string, error) {
	evidence, err := recordedCWDsFromClaudeProject(ctx, entryPath)
	if err != nil {
		return "", "", "", err
	}

	var project string
	var liveCWD string
	var absentCWD string
	var unverifiableCWD string
	absentCount := 0
	for _, cwd := range evidence.cwds {
		if err := ctx.Err(); err != nil {
			return "", "", "", err
		}
		if project == "" {
			project = detectProjectName(cwd)
		}
		if _, err := os.Stat(cwd); err == nil {
			if liveCWD == "" {
				liveCWD = cwd
			}
		} else if errors.Is(err, os.ErrNotExist) {
			absentCount++
			if absentCWD == "" {
				absentCWD = cwd
			}
		} else if unverifiableCWD == "" {
			unverifiableCWD = cwd
		}
	}

	if liveCWD != "" {
		return types.EntryClassLive,
			fmt.Sprintf("recorded cwd exists: %s (%d distinct recorded cwd(s) checked)", liveCWD, len(evidence.cwds)),
			project,
			nil
	}
	if unverifiableCWD != "" {
		return types.EntryClassUndetermined,
			"recorded cwd existence could not be verified: " + unverifiableCWD,
			project,
			nil
	}
	if len(evidence.unverifiableFiles) > 0 {
		if absentCount > 0 {
			return types.EntryClassUndetermined,
				fmt.Sprintf("%d recorded cwd(s) do not exist, but session metadata could not be verified: %s",
					absentCount, evidence.unverifiableFiles[0]),
				project,
				nil
		}
		return types.EntryClassUndetermined,
			"session metadata could not be verified: " + evidence.unverifiableFiles[0],
			project,
			nil
	}
	if len(evidence.cwds) == 0 {
		return types.EntryClassUndetermined, "no recorded cwd could be read from session metadata", project, nil
	}
	return types.EntryClassOrphaned,
		fmt.Sprintf("all %d distinct recorded cwd(s) do not exist; first: %s", len(evidence.cwds), absentCWD),
		project,
		nil
}

func recordedCWDsFromClaudeProject(ctx context.Context, entryPath string) (recordedCWDEvidence, error) {
	var evidence recordedCWDEvidence
	entries, err := os.ReadDir(entryPath)
	if err != nil {
		evidence.unverifiableFiles = append(evidence.unverifiableFiles, filepath.Base(entryPath))
		return evidence, nil
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return recordedCWDEvidence{}, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		cwds, err := readRecordedCWDs(ctx, filepath.Join(entryPath, entry.Name()))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return recordedCWDEvidence{}, err
			}
			evidence.unverifiableFiles = append(evidence.unverifiableFiles, entry.Name())
		}
		if len(cwds) == 0 && err == nil {
			evidence.unverifiableFiles = append(evidence.unverifiableFiles, entry.Name())
		}
		for _, cwd := range cwds {
			if !seen[cwd] {
				seen[cwd] = true
				evidence.cwds = append(evidence.cwds, cwd)
			}
		}
	}
	return evidence, nil
}

func readRecordedCWDs(ctx context.Context, path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var extractor cwdMetadataExtractor
	var cwds []string
	lineComplete := false
	for {
		if err := ctx.Err(); err != nil {
			return cwds, err
		}
		fragment, readErr := reader.ReadSlice('\n')
		if !lineComplete {
			if cwd := extractor.feed(fragment); cwd != "" {
				cwds = append(cwds, cwd)
				lineComplete = true
			}
		}
		switch {
		case readErr == nil:
			extractor.reset()
			lineComplete = false
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		case errors.Is(readErr, io.EOF):
			return cwds, nil
		default:
			return cwds, readErr
		}
	}
}

type cwdStringRole uint8

const (
	cwdStringIgnored cwdStringRole = iota
	cwdStringKey
	cwdStringValue
)

// cwdMetadataExtractor recognizes only a top-level "cwd" string. Other JSON
// values are skipped byte-by-byte and never decoded or retained.
type cwdMetadataExtractor struct {
	depth      int
	started    bool
	invalid    bool
	expectKey  bool
	pendingCWD bool
	afterColon bool
	inString   bool
	escaped    bool
	role       cwdStringRole
	keyMatched bool
	keyLength  int
	cwdRaw     []byte
	cwdTooLong bool
}

func (e *cwdMetadataExtractor) reset() {
	*e = cwdMetadataExtractor{}
}

func (e *cwdMetadataExtractor) feed(data []byte) string {
	for _, b := range data {
		if e.invalid {
			continue
		}
		if e.inString {
			if e.role == cwdStringValue && !e.cwdTooLong {
				e.cwdRaw = append(e.cwdRaw, b)
				if len(e.cwdRaw) > maxRecordedCWDBytes {
					e.cwdRaw = nil
					e.cwdTooLong = true
				}
			}
			if e.escaped {
				e.escaped = false
				if e.role == cwdStringKey {
					e.keyMatched = false
				}
				continue
			}
			if b == '\\' {
				e.escaped = true
				continue
			}
			if b != '"' {
				if e.role == cwdStringKey {
					e.matchKeyByte(b)
				}
				continue
			}

			e.inString = false
			switch e.role {
			case cwdStringKey:
				e.pendingCWD = e.keyMatched && e.keyLength == len("cwd")
				e.expectKey = false
			case cwdStringValue:
				if !e.cwdTooLong {
					var cwd string
					raw := append([]byte{'"'}, e.cwdRaw...)
					if json.Unmarshal(raw, &cwd) == nil && filepath.IsAbs(cwd) {
						return filepath.Clean(cwd)
					}
				}
				e.pendingCWD = false
				e.afterColon = false
			}
			e.role = cwdStringIgnored
			continue
		}

		if !e.started {
			if isJSONWhitespace(b) {
				continue
			}
			if b == '{' {
				e.started = true
				e.depth = 1
				e.expectKey = true
			} else {
				e.invalid = true
			}
			continue
		}

		switch b {
		case '"':
			e.inString = true
			e.escaped = false
			e.role = cwdStringIgnored
			if e.depth == 1 && e.expectKey {
				e.role = cwdStringKey
				e.keyMatched = true
				e.keyLength = 0
			} else if e.depth == 1 && e.pendingCWD && e.afterColon {
				e.role = cwdStringValue
				e.cwdRaw = e.cwdRaw[:0]
				e.cwdTooLong = false
			}
		case '{', '[':
			e.depth++
			if e.depth == 2 && e.pendingCWD && e.afterColon {
				e.pendingCWD = false
			}
		case '}', ']':
			if e.depth > 0 {
				e.depth--
			}
		case ':':
			if e.depth == 1 && !e.expectKey {
				e.afterColon = true
			}
		case ',':
			if e.depth == 1 {
				e.expectKey = true
				e.pendingCWD = false
				e.afterColon = false
			}
		default:
			if e.depth == 1 && e.pendingCWD && e.afterColon && !isJSONWhitespace(b) {
				e.pendingCWD = false
			}
		}
	}
	return ""
}

func (e *cwdMetadataExtractor) matchKeyByte(b byte) {
	const key = "cwd"
	if e.keyLength >= len(key) || key[e.keyLength] != b {
		e.keyMatched = false
	}
	e.keyLength++
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
