//go:build unix

package cmd

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
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("unsupported file identity metadata")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
