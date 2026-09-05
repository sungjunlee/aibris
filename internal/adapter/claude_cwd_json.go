package adapter

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
)

const maxRecordedCWDBytes = 64 * 1024

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
