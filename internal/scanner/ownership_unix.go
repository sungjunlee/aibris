//go:build !windows

package scanner

import (
	"os"
	"syscall"
)

// pathOwnedByCurrentUser reports whether the current user owns path. An
// unreadable path or unavailable uid metadata proves no ownership.
func pathOwnedByCurrentUser(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return int(stat.Uid) == os.Getuid()
}
