package codexsession

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
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
