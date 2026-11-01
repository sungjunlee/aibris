//go:build windows

package cleanjson

import (
	"os"
	"path/filepath"
	"syscall"
)

// openReceiptLeaf opens the receipt leaf without following a reparse point.
// A Windows named-pipe CreateFile returns immediately (a handle or
// ERROR_PIPE_BUSY) instead of waiting for a peer. A pipe or character
// device that does open is refused from that handle before anything is written.
func openReceiptLeaf(dir *os.File, name string, mode receiptLeafMode) (*os.File, error) {
	if err := receiptParentStillBound(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir.Name(), name)
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	access := uint32(syscall.GENERIC_READ)
	create := uint32(syscall.OPEN_EXISTING)
	switch mode {
	case receiptLeafProbe:
	case receiptLeafCreate:
		access = syscall.GENERIC_WRITE
		create = syscall.CREATE_NEW
	case receiptLeafWrite:
		access = syscall.GENERIC_WRITE
	default:
		return nil, os.ErrInvalid
	}
	// OPEN_REPARSE_POINT returns the alias itself. Writing that handle would
	// not truncate the referent, but it also must not be treated as success.
	handle, err := syscall.CreateFile(
		pointer,
		access,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		create,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	if fileIsReparse(file) {
		file.Close()
		return nil, errReceiptLeafAlias
	}
	return file, nil
}

func receiptParentStillBound(dir *os.File) error {
	if dir == nil {
		return os.ErrClosed
	}
	opened, err := dir.Stat()
	if err != nil {
		return err
	}
	current, err := os.Stat(dir.Name())
	if err != nil {
		return err
	}
	if !opened.IsDir() || !os.SameFile(opened, current) {
		return errReceiptLeafAlias
	}
	return nil
}

func fileIsReparse(file *os.File) bool {
	var info syscall.ByHandleFileInformation
	err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &info)
	if err != nil {
		return true
	}
	return info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
