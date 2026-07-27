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
	"strings"

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
	cwds                    []string
	unverifiableFiles       []string
	unverifiableRecords     int
	firstUnverifiableRecord string
}

// ClassifyClaudeProjectEntry re-derives the cleanup-driving classification
// from the current contents of a Claude project-store entry.
func ClassifyClaudeProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, error) {
	classification, _, _, err := classifyClaudeProjectEntry(ctx, entryPath)
	return classification, err
}

func classifyClaudeProjectEntry(ctx context.Context, entryPath string) (types.EntryClass, string, string, error) {
	evidence, err := recordedCWDsFromClaudeProject(ctx, entryPath)
	if err != nil {
		return "", "", "", err
	}

	var liveCWD string
	var absentCWD string
	var unverifiableCWD string
	var unavailableCWD string
	var unavailableAncestor string
	absentCount := 0
	for _, cwd := range evidence.cwds {
		if err := ctx.Err(); err != nil {
			return "", "", "", err
		}
		if _, err := os.Stat(cwd); err == nil {
			if liveCWD == "" {
				liveCWD = cwd
			}
		} else if errors.Is(err, os.ErrNotExist) {
			proven, ancestor, proofErr := recordedCWDAbsenceProven(cwd)
			switch {
			case proofErr != nil:
				if unverifiableCWD == "" {
					unverifiableCWD = cwd
				}
			case proven:
				absentCount++
				if absentCWD == "" {
					absentCWD = cwd
				}
			case unavailableCWD == "":
				unavailableCWD = cwd
				unavailableAncestor = ancestor
			}
		} else if unverifiableCWD == "" {
			unverifiableCWD = cwd
		}
	}

	if liveCWD != "" {
		return types.EntryClassLive,
			fmt.Sprintf("recorded cwd exists: %s (%d distinct recorded cwd(s) checked)", liveCWD, len(evidence.cwds)),
			projectNameFromRecordedCWD(liveCWD),
			nil
	}
	if unverifiableCWD != "" {
		return types.EntryClassUndetermined,
			"recorded cwd existence could not be verified: " + unverifiableCWD,
			projectNameFromRecordedCWD(unverifiableCWD),
			nil
	}
	if unavailableCWD != "" {
		return types.EntryClassUndetermined,
			fmt.Sprintf("recorded cwd surrounding tree is unavailable: %s (nearest existing ancestor: %s)",
				unavailableCWD, unavailableAncestor),
			projectNameFromRecordedCWD(unavailableCWD),
			nil
	}
	if evidence.unverifiableRecords > 0 {
		if absentCount > 0 {
			return types.EntryClassUndetermined,
				fmt.Sprintf("%d recorded cwd(s) do not exist, but %d session record(s) were unparseable or ended without a readable cwd; first: %s",
					absentCount, evidence.unverifiableRecords, evidence.firstUnverifiableRecord),
				projectNameFromRecordedCWD(absentCWD),
				nil
		}
		return types.EntryClassUndetermined,
			fmt.Sprintf("%d session record(s) were unparseable or ended without a readable cwd; first: %s",
				evidence.unverifiableRecords, evidence.firstUnverifiableRecord),
			"",
			nil
	}
	if len(evidence.unverifiableFiles) > 0 {
		if absentCount > 0 {
			return types.EntryClassUndetermined,
				fmt.Sprintf("%d recorded cwd(s) do not exist, but session metadata could not be verified: %s",
					absentCount, evidence.unverifiableFiles[0]),
				projectNameFromRecordedCWD(absentCWD),
				nil
		}
		return types.EntryClassUndetermined,
			"session metadata could not be verified: " + evidence.unverifiableFiles[0],
			"",
			nil
	}
	if len(evidence.cwds) == 0 {
		return types.EntryClassUndetermined, "no recorded cwd could be read from session metadata", "", nil
	}
	return types.EntryClassOrphaned,
		fmt.Sprintf("all %d distinct recorded cwd(s) do not exist; first: %s", len(evidence.cwds), absentCWD),
		projectNameFromRecordedCWD(absentCWD),
		nil
}

// recordedCWDAbsenceProven accepts ENOENT as deletion evidence only when the
// closest existing directory is in a container whose surrounding tree is
// expected to be locally available: the user's home or a temp root.
func recordedCWDAbsenceProven(cwd string) (bool, string, error) {
	for ancestor := cwd; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Lstat(ancestor)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(ancestor)
				if err != nil {
					return false, ancestor, nil
				}
			}
			if !info.IsDir() {
				return false, ancestor, nil
			}
			plausible, err := plausibleRecordedCWDContainer(ancestor)
			return plausible, ancestor, err
		case !errors.Is(err, os.ErrNotExist):
			return false, ancestor, err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false, ancestor, nil
		}
	}
}

func plausibleRecordedCWDContainer(path string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	roots := []string{home, os.TempDir()}
	if filepath.Separator == '/' {
		roots = append(roots, "/tmp", "/var/tmp")
	}
	for _, root := range roots {
		if pathWithinContainer(path, root) {
			return true, nil
		}
	}
	return false, nil
}

func pathWithinContainer(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(resolved)
	}
	rel, err := filepath.Rel(root, path)
	return err == nil &&
		(rel == "." || (rel != ".." && !filepath.IsAbs(rel) &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func recordedCWDsFromClaudeProject(ctx context.Context, entryPath string) (recordedCWDEvidence, error) {
	var evidence recordedCWDEvidence
	entries, err := os.ReadDir(entryPath)
	if err != nil {
		evidence.unverifiableFiles = append(evidence.unverifiableFiles, filepath.Base(entryPath))
		// Deliberately fail closed: unreadable project metadata is unverifiable
		// evidence for this entry, not a reason to abort the entire scan.
		return evidence, nil //nolint:nilerr
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return recordedCWDEvidence{}, err
		}
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonl" {
			continue
		}
		readResult, err := readRecordedCWDs(ctx, filepath.Join(entryPath, entry.Name()))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return recordedCWDEvidence{}, err
			}
			evidence.unverifiableFiles = append(evidence.unverifiableFiles, entry.Name())
		}
		if readResult.unverifiableRecords > 0 {
			evidence.unverifiableRecords += readResult.unverifiableRecords
			if evidence.firstUnverifiableRecord == "" {
				evidence.firstUnverifiableRecord = fmt.Sprintf("%s:%d",
					entry.Name(), readResult.firstUnverifiableLine)
			}
		}
		for _, cwd := range readResult.cwds {
			if !seen[cwd] {
				seen[cwd] = true
				evidence.cwds = append(evidence.cwds, cwd)
			}
		}
	}
	return evidence, nil
}

type recordedCWDReadResult struct {
	cwds                  []string
	unverifiableRecords   int
	firstUnverifiableLine int
}

// readRecordedCWDs returns usable cwd metadata plus a count of malformed or
// truncated records. Valid JSONL events without a cwd are not working-directory
// evidence, including when every record in the file is cwd-less.
func readRecordedCWDs(ctx context.Context, path string) (recordedCWDReadResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return recordedCWDReadResult{}, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReader(file)
	var extractor cwdMetadataExtractor
	var result recordedCWDReadResult
	cwdFound := false
	lineNumber := 1
	recordUnverifiable := func() {
		if !extractor.unverifiableRecord(cwdFound) {
			return
		}
		result.unverifiableRecords++
		if result.firstUnverifiableLine == 0 {
			result.firstUnverifiableLine = lineNumber
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		fragment, readErr := reader.ReadSlice('\n')
		if cwd := extractor.feed(fragment); cwd != "" {
			if !cwdFound {
				result.cwds = append(result.cwds, cwd)
				cwdFound = true
			}
		}
		switch {
		case readErr == nil:
			recordUnverifiable()
			extractor.reset()
			cwdFound = false
			lineNumber++
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		case errors.Is(readErr, io.EOF):
			recordUnverifiable()
			return result, nil
		default:
			return result, readErr
		}
	}
}

type jsonContainerState uint8

const (
	jsonObjectKeyOrEnd jsonContainerState = iota
	jsonObjectKey
	jsonObjectColon
	jsonObjectValue
	jsonObjectCommaOrEnd
	jsonArrayValueOrEnd
	jsonArrayValue
	jsonArrayCommaOrEnd
)

type jsonContainer struct {
	kind     byte
	state    jsonContainerState
	keyIsCWD bool
}

type jsonNumberState uint8

const (
	jsonNumberNone jsonNumberState = iota
	jsonNumberAfterMinus
	jsonNumberZero
	jsonNumberInteger
	jsonNumberAfterDot
	jsonNumberFraction
	jsonNumberAfterExponent
	jsonNumberAfterExponentSign
	jsonNumberExponent
)

// cwdMetadataExtractor validates JSON incrementally until it recognizes a
// top-level "cwd" string. Other values are never decoded or retained.
type cwdMetadataExtractor struct {
	containers       []jsonContainer
	started          bool
	complete         bool
	invalid          bool
	recordContent    bool
	inString         bool
	stringIsKey      bool
	stringIsCWD      bool
	escaped          bool
	unicodeDigits    int
	keyMatched       bool
	keyLength        int
	cwdFieldSeen     bool
	cwdRaw           []byte
	cwdTooLong       bool
	literalRemaining string
	numberState      jsonNumberState
}

func (e *cwdMetadataExtractor) reset() {
	*e = cwdMetadataExtractor{}
}

func (e *cwdMetadataExtractor) unverifiableRecord(cwdFound bool) bool {
	// A complete JSON object can legitimately be a cwd-less event. Incomplete
	// or invalid JSON, or a present but unusable cwd field, is unreadable
	// evidence. A parsed cwd does not make a malformed surrounding record valid.
	return e.recordContent && (e.invalid || !e.complete || (e.cwdFieldSeen && !cwdFound))
}

func (e *cwdMetadataExtractor) feed(data []byte) string {
	var foundCWD string
	for i := 0; i < len(data); {
		b := data[i]
		if !isJSONWhitespace(b) {
			e.recordContent = true
		}
		if e.invalid {
			i++
			continue
		}
		if e.inString {
			if cwd := e.feedStringByte(b); cwd != "" {
				if foundCWD == "" {
					foundCWD = cwd
				}
			}
			i++
			continue
		}
		if e.literalRemaining != "" {
			if b != e.literalRemaining[0] {
				e.invalid = true
			} else {
				e.literalRemaining = e.literalRemaining[1:]
				if e.literalRemaining == "" {
					e.finishValue()
				}
			}
			i++
			continue
		}
		if e.numberState != jsonNumberNone {
			if e.feedNumberByte(b) {
				i++
				continue
			}
			if !e.numberCanEnd() || !isJSONValueDelimiter(b) {
				e.invalid = true
				i++
				continue
			}
			e.numberState = jsonNumberNone
			e.finishValue()
			continue
		}
		if e.complete {
			if !isJSONWhitespace(b) {
				e.invalid = true
			}
			i++
			continue
		}
		if !e.started {
			if isJSONWhitespace(b) {
				i++
				continue
			}
			if b != '{' {
				e.invalid = true
				i++
				continue
			}
			e.started = true
			e.containers = append(e.containers, jsonContainer{
				kind:  '{',
				state: jsonObjectKeyOrEnd,
			})
			i++
			continue
		}
		if isJSONWhitespace(b) {
			i++
			continue
		}
		if len(e.containers) == 0 {
			e.invalid = true
			i++
			continue
		}

		frame := &e.containers[len(e.containers)-1]
		switch frame.state {
		case jsonObjectKeyOrEnd:
			if b == '}' {
				e.closeContainer('}')
			} else if b == '"' {
				e.startKeyString()
			} else {
				e.invalid = true
			}
		case jsonObjectKey:
			if b == '"' {
				e.startKeyString()
			} else {
				e.invalid = true
			}
		case jsonObjectColon:
			if b == ':' {
				frame.state = jsonObjectValue
			} else {
				e.invalid = true
			}
		case jsonObjectValue, jsonArrayValueOrEnd, jsonArrayValue:
			if frame.state == jsonArrayValueOrEnd && b == ']' {
				e.closeContainer(']')
			} else {
				e.startValue(b)
			}
		case jsonObjectCommaOrEnd:
			if b == ',' {
				frame.state = jsonObjectKey
			} else if b == '}' {
				e.closeContainer('}')
			} else {
				e.invalid = true
			}
		case jsonArrayCommaOrEnd:
			if b == ',' {
				frame.state = jsonArrayValue
			} else if b == ']' {
				e.closeContainer(']')
			} else {
				e.invalid = true
			}
		}
		i++
	}
	return foundCWD
}

func (e *cwdMetadataExtractor) startKeyString() {
	e.inString = true
	e.stringIsKey = true
	e.stringIsCWD = false
	e.escaped = false
	e.unicodeDigits = 0
	e.keyMatched = len(e.containers) == 1
	e.keyLength = 0
}

func (e *cwdMetadataExtractor) startValue(b byte) {
	switch b {
	case '"':
		frame := &e.containers[len(e.containers)-1]
		e.inString = true
		e.stringIsKey = false
		e.stringIsCWD = len(e.containers) == 1 && frame.kind == '{' && frame.keyIsCWD
		e.escaped = false
		e.unicodeDigits = 0
		e.cwdRaw = e.cwdRaw[:0]
		e.cwdTooLong = false
		if e.stringIsCWD {
			e.cwdRaw = append(e.cwdRaw, '"')
		}
	case '{':
		e.containers = append(e.containers, jsonContainer{
			kind:  '{',
			state: jsonObjectKeyOrEnd,
		})
	case '[':
		e.containers = append(e.containers, jsonContainer{
			kind:  '[',
			state: jsonArrayValueOrEnd,
		})
	case 't':
		e.literalRemaining = "rue"
	case 'f':
		e.literalRemaining = "alse"
	case 'n':
		e.literalRemaining = "ull"
	case '-':
		e.numberState = jsonNumberAfterMinus
	case '0':
		e.numberState = jsonNumberZero
	default:
		if b >= '1' && b <= '9' {
			e.numberState = jsonNumberInteger
		} else {
			e.invalid = true
		}
	}
}

func (e *cwdMetadataExtractor) feedStringByte(b byte) string {
	if e.stringIsCWD && !e.cwdTooLong {
		e.cwdRaw = append(e.cwdRaw, b)
		if len(e.cwdRaw) > maxRecordedCWDBytes {
			e.cwdRaw = nil
			e.cwdTooLong = true
		}
	}
	if e.unicodeDigits > 0 {
		if !isJSONHexDigit(b) {
			e.invalid = true
			return ""
		}
		e.unicodeDigits--
		return ""
	}
	if e.escaped {
		e.escaped = false
		if e.stringIsKey {
			e.keyMatched = false
		}
		switch b {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		case 'u':
			e.unicodeDigits = 4
		default:
			e.invalid = true
		}
		return ""
	}
	if b == '\\' {
		e.escaped = true
		return ""
	}
	if b < 0x20 {
		e.invalid = true
		return ""
	}
	if b != '"' {
		if e.stringIsKey {
			e.matchKeyByte(b)
		}
		return ""
	}

	e.inString = false
	if e.stringIsKey {
		frame := &e.containers[len(e.containers)-1]
		frame.keyIsCWD = e.keyMatched && e.keyLength == len("cwd")
		if frame.keyIsCWD {
			e.cwdFieldSeen = true
		}
		frame.state = jsonObjectColon
		return ""
	}
	if e.stringIsCWD && !e.cwdTooLong {
		var cwd string
		if json.Unmarshal(e.cwdRaw, &cwd) == nil && filepath.IsAbs(cwd) {
			e.finishValue()
			return filepath.Clean(cwd)
		}
	}
	e.finishValue()
	return ""
}

func (e *cwdMetadataExtractor) closeContainer(closing byte) {
	frame := e.containers[len(e.containers)-1]
	if (closing == '}' && frame.kind != '{') || (closing == ']' && frame.kind != '[') {
		e.invalid = true
		return
	}
	e.containers = e.containers[:len(e.containers)-1]
	if len(e.containers) == 0 {
		e.complete = true
		return
	}
	e.finishValue()
}

func (e *cwdMetadataExtractor) finishValue() {
	if len(e.containers) == 0 {
		e.complete = true
		return
	}
	frame := &e.containers[len(e.containers)-1]
	switch frame.state {
	case jsonObjectValue:
		frame.state = jsonObjectCommaOrEnd
		frame.keyIsCWD = false
	case jsonArrayValueOrEnd, jsonArrayValue:
		frame.state = jsonArrayCommaOrEnd
	default:
		e.invalid = true
	}
}

func (e *cwdMetadataExtractor) feedNumberByte(b byte) bool {
	switch e.numberState {
	case jsonNumberAfterMinus:
		if b == '0' {
			e.numberState = jsonNumberZero
			return true
		}
		if b >= '1' && b <= '9' {
			e.numberState = jsonNumberInteger
			return true
		}
	case jsonNumberZero:
		if b == '.' {
			e.numberState = jsonNumberAfterDot
			return true
		}
		if b == 'e' || b == 'E' {
			e.numberState = jsonNumberAfterExponent
			return true
		}
	case jsonNumberInteger:
		if b >= '0' && b <= '9' {
			return true
		}
		if b == '.' {
			e.numberState = jsonNumberAfterDot
			return true
		}
		if b == 'e' || b == 'E' {
			e.numberState = jsonNumberAfterExponent
			return true
		}
	case jsonNumberAfterDot:
		if b >= '0' && b <= '9' {
			e.numberState = jsonNumberFraction
			return true
		}
	case jsonNumberFraction:
		if b >= '0' && b <= '9' {
			return true
		}
		if b == 'e' || b == 'E' {
			e.numberState = jsonNumberAfterExponent
			return true
		}
	case jsonNumberAfterExponent:
		if b == '+' || b == '-' {
			e.numberState = jsonNumberAfterExponentSign
			return true
		}
		if b >= '0' && b <= '9' {
			e.numberState = jsonNumberExponent
			return true
		}
	case jsonNumberAfterExponentSign:
		if b >= '0' && b <= '9' {
			e.numberState = jsonNumberExponent
			return true
		}
	case jsonNumberExponent:
		if b >= '0' && b <= '9' {
			return true
		}
	}
	return false
}

func (e *cwdMetadataExtractor) numberCanEnd() bool {
	return e.numberState == jsonNumberZero ||
		e.numberState == jsonNumberInteger ||
		e.numberState == jsonNumberFraction ||
		e.numberState == jsonNumberExponent
}

func (e *cwdMetadataExtractor) matchKeyByte(b byte) {
	const key = "cwd"
	if e.keyLength >= len(key) || key[e.keyLength] != b {
		e.keyMatched = false
	}
	e.keyLength++
}

func isJSONValueDelimiter(b byte) bool {
	return isJSONWhitespace(b) || b == ',' || b == '}' || b == ']'
}

func isJSONHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'f') ||
		(b >= 'A' && b <= 'F')
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
