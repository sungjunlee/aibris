package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
		classification, reason, err := classifyClaudeProjectEntry(ctx, entryPath)
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
			Path:           entryPath,
			Size:           size,
			ModTime:        info.ModTime(),
			Classification: classification,
			Reason:         reason,
		})
	}
	return results, nil
}

func classifyClaudeProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, string, error) {
	cwd, err := recordedCWDFromClaudeProject(ctx, entryPath)
	if err != nil {
		return "", "", err
	}
	if cwd == "" {
		return types.EntryClassUndetermined, "no recorded cwd could be read from session metadata", nil
	}

	if _, err := os.Stat(cwd); err == nil {
		return types.EntryClassLive, "recorded cwd exists: " + cwd, nil
	} else if errors.Is(err, os.ErrNotExist) {
		return types.EntryClassOrphaned, "recorded cwd does not exist: " + cwd, nil
	}
	return types.EntryClassUndetermined, "recorded cwd existence could not be verified: " + cwd, nil
}

func recordedCWDFromClaudeProject(ctx context.Context, entryPath string) (string, error) {
	entries, err := os.ReadDir(entryPath)
	if err != nil {
		return "", nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		cwd, err := readRecordedCWD(ctx, filepath.Join(entryPath, entry.Name()))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			continue
		}
		if cwd != "" {
			return cwd, nil
		}
	}
	return "", nil
}

func readRecordedCWD(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var extractor cwdMetadataExtractor
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fragment, readErr := reader.ReadSlice('\n')
		if cwd := extractor.feed(fragment); cwd != "" {
			return cwd, nil
		}
		switch {
		case readErr == nil:
			extractor.reset()
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		case errors.Is(readErr, io.EOF):
			return "", nil
		default:
			return "", readErr
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
