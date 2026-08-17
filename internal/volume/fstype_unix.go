//go:build unix && !darwin

package volume

import "syscall"

func unixFSType(_ syscall.Statfs_t) string {
	return "unix"
}
