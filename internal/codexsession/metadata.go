package codexsession

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"unicode/utf8"
)

const MaxFirstRecordBytes = 64 * 1024

type ErrorKind string

const (
	ErrorEmpty        ErrorKind = "empty"
	ErrorOversized    ErrorKind = "oversized"
	ErrorTruncated    ErrorKind = "truncated"
	ErrorMalformed    ErrorKind = "malformed"
	ErrorWrongRecord  ErrorKind = "wrong-record"
	ErrorAmbiguous    ErrorKind = "ambiguous"
	ErrorInvalidField ErrorKind = "invalid-field"
)

// ParseError describes only the shape of invalid first-record evidence. It
// never includes a raw line, path, cwd, identifier, or metadata value.
type ParseError struct {
	Kind ErrorKind
}

func (e *ParseError) Error() string {
	return "invalid Codex session metadata: " + string(e.Kind)
}

// Metadata contains only the scalar first-record fields used by aibris.
// Unknown fields are validated and discarded without being retained.
type Metadata struct {
	Timestamp          string
	CWD                string
	HasSessionIdentity bool
	Producer           string
	Version            string
}

// ReadFirstMetadata reads exactly one newline-terminated JSONL record under a
// strict byte cap. No second record is read or decoded.
func ReadFirstMetadata(ctx context.Context, path string) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { _ = file.Close() }()

	return ReadFirstMetadataFrom(ctx, file)
}

// ReadFirstMetadataFrom reads metadata from an already-open session handle.
// Callers that need no-follow or physical-identity guarantees can therefore
// bind parsing to the same handle they inspected.
func ReadFirstMetadataFrom(ctx context.Context, source io.Reader) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	reader := bufio.NewReader(io.LimitReader(source, MaxFirstRecordBytes+1))
	line, readErr := reader.ReadBytes('\n')
	switch {
	case readErr == nil:
	case errors.Is(readErr, io.EOF):
		if len(line) == 0 {
			return Metadata{}, &ParseError{Kind: ErrorEmpty}
		}
		if len(line) > MaxFirstRecordBytes {
			return Metadata{}, &ParseError{Kind: ErrorOversized}
		}
		return Metadata{}, &ParseError{Kind: ErrorTruncated}
	default:
		return Metadata{}, readErr
	}
	if len(line) > MaxFirstRecordBytes {
		return Metadata{}, &ParseError{Kind: ErrorOversized}
	}
	if len(line) == 1 {
		return Metadata{}, &ParseError{Kind: ErrorEmpty}
	}
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}

	metadata, err := decodeMetadata(line[:len(line)-1])
	if err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func decodeMetadata(record []byte) (Metadata, error) {
	if !utf8.Valid(record) {
		return Metadata{}, &ParseError{Kind: ErrorMalformed}
	}
	if hasUnpairedSurrogateEscape(record) {
		return Metadata{}, &ParseError{Kind: ErrorMalformed}
	}
	decoder := json.NewDecoder(bytes.NewReader(record))
	token, err := decoder.Token()
	if err != nil {
		return Metadata{}, &ParseError{Kind: ErrorMalformed}
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return Metadata{}, &ParseError{Kind: ErrorMalformed}
	}

	var metadata Metadata
	seen := make(map[string]bool)
	var recordType string
	for decoder.More() {
		key, err := objectKey(decoder)
		if err != nil {
			return Metadata{}, err
		}
		switch key {
		case "type":
			if duplicate(seen, key) {
				return Metadata{}, &ParseError{Kind: ErrorAmbiguous}
			}
			recordType, err = scalarString(decoder)
		case "timestamp":
			if duplicate(seen, key) {
				return Metadata{}, &ParseError{Kind: ErrorAmbiguous}
			}
			metadata.Timestamp, err = scalarString(decoder)
		case "payload":
			if duplicate(seen, key) {
				return Metadata{}, &ParseError{Kind: ErrorAmbiguous}
			}
			err = decodePayload(decoder, &metadata)
		default:
			err = discardValue(decoder, 0)
		}
		if err != nil {
			return Metadata{}, err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return Metadata{}, &ParseError{Kind: ErrorMalformed}
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return Metadata{}, &ParseError{Kind: ErrorMalformed}
	}
	if recordType != "session_meta" {
		return Metadata{}, &ParseError{Kind: ErrorWrongRecord}
	}
	return metadata, nil
}

func decodePayload(decoder *json.Decoder, metadata *Metadata) error {
	token, err := decoder.Token()
	if err != nil {
		return &ParseError{Kind: ErrorMalformed}
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return &ParseError{Kind: ErrorInvalidField}
	}

	seen := make(map[string]bool)
	for decoder.More() {
		key, err := objectKey(decoder)
		if err != nil {
			return err
		}
		var destination *string
		switch key {
		case "cwd":
			destination = &metadata.CWD
		case "session_id", "id", "thread_id":
			if duplicate(seen, key) {
				return &ParseError{Kind: ErrorAmbiguous}
			}
			value, err := scalarString(decoder)
			if err != nil {
				return err
			}
			if value != "" {
				metadata.HasSessionIdentity = true
			}
			continue
		case "originator":
			destination = &metadata.Producer
		case "cli_version":
			destination = &metadata.Version
		}
		if destination == nil {
			if err := discardValue(decoder, 0); err != nil {
				return err
			}
			continue
		}
		if duplicate(seen, key) {
			return &ParseError{Kind: ErrorAmbiguous}
		}
		value, err := scalarString(decoder)
		if err != nil {
			return err
		}
		*destination = value
	}
	if _, err := decoder.Token(); err != nil {
		return &ParseError{Kind: ErrorMalformed}
	}
	return nil
}

func objectKey(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", &ParseError{Kind: ErrorMalformed}
	}
	key, ok := token.(string)
	if !ok {
		return "", &ParseError{Kind: ErrorMalformed}
	}
	return key, nil
}

func scalarString(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", &ParseError{Kind: ErrorMalformed}
	}
	value, ok := token.(string)
	if !ok {
		return "", &ParseError{Kind: ErrorInvalidField}
	}
	return value, nil
}

func duplicate(seen map[string]bool, key string) bool {
	if seen[key] {
		return true
	}
	seen[key] = true
	return false
}

func discardValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return &ParseError{Kind: ErrorMalformed}
	}
	token, err := decoder.Token()
	if err != nil {
		return &ParseError{Kind: ErrorMalformed}
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := objectKey(decoder); err != nil {
				return err
			}
			if err := discardValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := discardValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return &ParseError{Kind: ErrorMalformed}
	}
	if _, err := decoder.Token(); err != nil {
		return &ParseError{Kind: ErrorMalformed}
	}
	return nil
}

func hasUnpairedSurrogateEscape(record []byte) bool {
	inString := false
	for index := 0; index < len(record); index++ {
		switch record[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(record) {
				continue
			}
			if record[index+1] != 'u' {
				index++
				continue
			}
			value, ok := hexQuad(record, index+2)
			if !ok {
				continue
			}
			switch {
			case value >= 0xd800 && value <= 0xdbff:
				paired, pairOK := hexQuad(record, index+8)
				if index+7 >= len(record) ||
					record[index+6] != '\\' ||
					record[index+7] != 'u' ||
					!pairOK ||
					paired < 0xdc00 ||
					paired > 0xdfff {
					return true
				}
				index += 11
			case value >= 0xdc00 && value <= 0xdfff:
				return true
			default:
				index += 5
			}
		}
	}
	return false
}

func hexQuad(record []byte, start int) (uint16, bool) {
	if start+4 > len(record) {
		return 0, false
	}
	var value uint16
	for _, digit := range record[start : start+4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value += uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value += uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value += uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func ErrorKindOf(err error) (ErrorKind, bool) {
	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		return "", false
	}
	return parseErr.Kind, true
}

func IsEvidenceError(err error) bool {
	_, ok := ErrorKindOf(err)
	return ok
}

func (m Metadata) HasActivityFields() bool {
	return m.HasSessionIdentity && m.Timestamp != "" && m.CWD != ""
}
