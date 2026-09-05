package cleanjson

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// WriteOwnerOnlyJSON stores one JSON value at path. The file is created or
// truncated and forced to owner-only 0600 so a pre-existing world-readable
// sink cannot keep its old permissions after gaining the document.
func WriteOwnerOnlyJSON(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	// The open mode only applies to a file this call creates, so an existing
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

func encodeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// ResolveReceiptSink normalizes the requested sink for containment
// comparison. The file need not exist yet, so only its parent directory can be
// resolved through symlinks.
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
	return filepath.Join(filepath.Clean(parent), filepath.Base(absolute)), nil
}

// RejectReceiptSinkOverlap refuses a sink that is, or lives inside, a
// target this run is about to remove. Writing there would recreate the path
// after its removal, so the receipt would claim a target was removed while it
// exists again.
func RejectReceiptSinkOverlap(path string, targets []types.DebrisInfo) error {
	sink, err := ResolveReceiptSink(path)
	if err != nil {
		return fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
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
