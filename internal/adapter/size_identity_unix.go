//go:build !windows

package adapter

import (
	"os"
	"syscall"
)

func hardlinkSizeIdentity(info os.FileInfo) (sizeFileIdentity, bool) {
	if !info.Mode().IsRegular() {
		return sizeFileIdentity{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil || stat.Nlink <= 1 {
		return sizeFileIdentity{}, false
	}
	return sizeFileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, true
}
