//go:build unix

package cleanjson

import (
	"os"

	"golang.org/x/sys/unix"
)

func openReceiptLeaf(dir *os.File, name string, mode receiptLeafMode) (*os.File, error) {
	fd, err := receiptDirFD(dir)
	if err != nil {
		return nil, err
	}
	flags := unix.O_NOFOLLOW | unix.O_CLOEXEC
	perm := uint32(0)
	switch mode {
	case receiptLeafProbe:
		flags |= unix.O_RDONLY
	case receiptLeafCreate:
		flags |= unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL
		perm = 0o600
	case receiptLeafWrite:
		flags |= unix.O_WRONLY
	default:
		return nil, os.ErrInvalid
	}
	leaf, err := unix.Openat(fd, name, flags, perm)
	if err != nil {
		if err == unix.ELOOP {
			return nil, errReceiptLeafAlias
		}
		return nil, err
	}
	// Fd() on the directory cleared close-on-exec. Put it back before a
	// cleanup command is spawned while this sink is still bound.
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(leaf), name)
	info, statErr := file.Stat()
	if statErr != nil {
		file.Close()
		return nil, statErr
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		file.Close()
		return nil, errReceiptLeafAlias
	}
	return file, nil
}

func receiptDirFD(dir *os.File) (int, error) {
	if dir == nil {
		return -1, os.ErrClosed
	}
	fd := int(dir.Fd())
	if fd < 0 {
		return -1, os.ErrClosed
	}
	unix.CloseOnExec(fd)
	return fd, nil
}
