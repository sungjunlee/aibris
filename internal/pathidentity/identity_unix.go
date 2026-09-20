//go:build unix

package pathidentity

import (
	"fmt"
	"os"
	"syscall"
)

func platformCleanupPathIdentity(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	sys := info.Sys()
	if sys == nil {
		return "", fmt.Errorf("file identity unavailable (Sys returned nil)")
	}
	stat, ok := sys.(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("file identity unavailable (Sys not *syscall.Stat_t)")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
