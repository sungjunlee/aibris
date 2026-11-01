package cleanjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// errReceiptLeafAlias refuses a symlink or reparse-point receipt leaf.
// Opening that leaf with O_CREATE|O_TRUNC would follow it and could recreate
// a target this run is about to remove.
var errReceiptLeafAlias = errors.New("symlink or reparse point")

// errReceiptLeafNotRegular refuses a leaf that is not a regular file, such
// as a directory, device, or named pipe.
var errReceiptLeafNotRegular = errors.New("not a regular file")

type receiptLeafMode int

const (
	receiptLeafProbe receiptLeafMode = iota
	receiptLeafCreate
	receiptLeafWrite
)

// ReceiptSink is a --receipt-file destination checked against the cleanup
// targets and bound to that directory before mutation. Writes go through the
// bound directory, without following a leaf alias, so a symlink swapped in
// after the check cannot redirect the document.
type ReceiptSink struct {
	path     string
	name     string
	dir      *os.File
	existed  bool
	identity os.FileInfo
}

// WriteOwnerOnlyJSON stores one JSON value at path. The file is created or
// truncated and forced to owner-only 0600 so a pre-existing world-readable
// sink cannot keep its old permissions after gaining the document. A symlink,
// reparse point, or other non-regular leaf is refused.
func WriteOwnerOnlyJSON(path string, value any) error {
	sink, err := PrepareReceiptSink(path, nil)
	if err != nil {
		return err
	}
	defer sink.Close()
	return sink.Write(value)
}

// PrepareReceiptSink resolves path, refuses a non-regular or aliased leaf, and
// refuses a sink that is or lives inside a target this run is about to remove.
// The returned sink holds the directory that was checked; Close releases it.
func PrepareReceiptSink(path string, targets []types.DebrisInfo) (*ReceiptSink, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	absolute = filepath.Clean(absolute)
	name := filepath.Base(absolute)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return nil, fmt.Errorf("receipt file %q is not a file path", path)
	}
	parent := filepath.Dir(absolute)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return nil, fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	resolvedParent = filepath.Clean(resolvedParent)
	dir, err := os.Open(resolvedParent)
	if err != nil {
		return nil, fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	sink := &ReceiptSink{path: path, name: name, dir: dir}
	opened, err := dir.Stat()
	if err != nil || !opened.IsDir() {
		sink.Close()
		if err == nil {
			err = fmt.Errorf("receipt parent %q is not a directory", resolvedParent)
		}
		return nil, fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	current, err := os.Stat(resolvedParent)
	if err != nil || !os.SameFile(opened, current) {
		sink.Close()
		return nil, fmt.Errorf("receipt file %q changed while it was checked", path)
	}

	file, err := openReceiptLeaf(dir, name, receiptLeafProbe)
	if receiptLeafAlias(err) {
		sink.Close()
		return nil, fmt.Errorf("receipt file %q is a symlink or reparse point", path)
	}
	if receiptLeafNotRegular(err) {
		sink.Close()
		return nil, fmt.Errorf("receipt file %q is not a regular file", path)
	}
	if err != nil && !os.IsNotExist(err) {
		sink.Close()
		return nil, fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	if err == nil {
		info, statErr := file.Stat()
		file.Close()
		if statErr != nil {
			sink.Close()
			return nil, fmt.Errorf("resolving receipt file %q: %w", path, statErr)
		}
		if !info.Mode().IsRegular() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			sink.Close()
			if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return nil, fmt.Errorf("receipt file %q is a symlink or reparse point", path)
			}
			return nil, fmt.Errorf("receipt file %q is not a regular file", path)
		}
		sink.existed = true
		sink.identity = info
	}
	if err := rejectResolvedSinkOverlap(path, filepath.Join(resolvedParent, name), targets); err != nil {
		sink.Close()
		return nil, err
	}
	return sink, nil
}

// Write stores value in the bound sink. An existing leaf must still be the
// file that was checked. A leaf created or replaced by an alias is not followed.
func (s *ReceiptSink) Write(value any) error {
	if s == nil || s.dir == nil {
		return errors.New("receipt file is not open")
	}
	var (
		file *os.File
		err  error
	)
	if s.existed {
		file, err = openReceiptLeaf(s.dir, s.name, receiptLeafWrite)
		if err != nil {
			return fmt.Errorf("opening receipt file %q: %w", s.path, err)
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || !os.SameFile(s.identity, info) {
			file.Close()
			return fmt.Errorf("receipt file %q changed after it was checked", s.path)
		}
		if err := file.Truncate(0); err != nil {
			file.Close()
			return err
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			file.Close()
			return err
		}
	} else {
		file, err = openReceiptLeaf(s.dir, s.name, receiptLeafCreate)
		if err != nil {
			return fmt.Errorf("creating receipt file %q: %w", s.path, err)
		}
	}
	// The create mode only applies to a file this call creates, so an existing
	// sink would keep whatever permissions it already had while gaining the
	// document's contents.
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if err := encodeJSON(file, value); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Close releases the bound directory. It is safe to call more than once.
func (s *ReceiptSink) Close() error {
	if s == nil || s.dir == nil {
		return nil
	}
	err := s.dir.Close()
	s.dir = nil
	return err
}

func receiptLeafAlias(err error) bool {
	return errors.Is(err, errReceiptLeafAlias)
}

func receiptLeafNotRegular(err error) bool {
	return errors.Is(err, errReceiptLeafNotRegular)
}

func encodeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// ResolveReceiptSink normalizes the requested sink for containment
// comparison. The parent directory is resolved through symlinks. A symlink or
// reparse-point leaf is refused instead of followed: the file need not exist
// yet, but an existing alias is not a safe place to write.
func ResolveReceiptSink(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return absolute, nil
	}
	sink := filepath.Join(filepath.Clean(parent), filepath.Base(absolute))
	info, err := os.Lstat(sink)
	if err != nil {
		if os.IsNotExist(err) {
			return sink, nil
		}
		return "", err
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return "", fmt.Errorf("receipt file %q is a symlink or reparse point", path)
	}
	return sink, nil
}

// RejectReceiptSinkOverlap refuses a sink that is, or lives inside, a
// target this run is about to remove. Writing there would recreate the path
// after its removal, so the receipt would claim a target was removed while it
// exists again. A leaf alias is refused even when its path is outside the
// target, because following it would write the document onto the referent.
func RejectReceiptSinkOverlap(path string, targets []types.DebrisInfo) error {
	sink, err := ResolveReceiptSink(path)
	if err != nil {
		return fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	return rejectResolvedSinkOverlap(path, sink, targets)
}

func rejectResolvedSinkOverlap(path, sink string, targets []types.DebrisInfo) error {
	for _, target := range targets {
		targetPath, ok := cleaner.TargetPathKey(target.Path)
		if !ok {
			continue
		}
		if sink == targetPath || cleaner.PathContains(targetPath, sink) {
			return fmt.Errorf("receipt file %q is inside a cleanup target", path)
		}
	}
	return nil
}
