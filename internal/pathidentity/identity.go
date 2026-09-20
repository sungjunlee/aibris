package pathidentity

import (
	"fmt"
	"os"
)

// PathIdentity returns the file info and platform-specific identity for a path.
// It fails if the path is a symlink or if the identity cannot be captured.
func PathIdentity(path string) (os.FileInfo, string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("symbolic-link cleanup targets are not cacheable")
	}
	identity, err := platformCleanupPathIdentity(path)
	if err != nil {
		return nil, "", err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !os.SameFile(before, after) || before.Mode().Type() != after.Mode().Type() {
		return nil, "", fmt.Errorf("path changed while capturing identity")
	}
	return after, identity, nil
}
