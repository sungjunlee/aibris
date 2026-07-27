//go:build !windows

package adapter

import (
	"fmt"
	"os"
	"syscall"
)

var recordedCWDDeviceID = func(_ string, info os.FileInfo) (uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("recorded cwd device metadata unavailable")
	}
	return uint64(stat.Dev), nil
}
