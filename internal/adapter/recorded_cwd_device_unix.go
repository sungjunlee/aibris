//go:build !windows

package adapter

import (
	"fmt"
	"os"
	"syscall"
)

var recordedCWDVolumeID = func(_ string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("recorded cwd device metadata unavailable")
	}
	return fmt.Sprintf("device:%d", uint64(stat.Dev)), nil
}
