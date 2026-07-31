//go:build windows

package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const windowsVolumePathBufferLength = 32768

var getVolumePathNameW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetVolumePathNameW")

var recordedCWDVolumeID = func(path string, _ os.FileInfo) (string, error) {
	pathPtr, err := syscall.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("encoding recorded cwd path for volume lookup: %w", err)
	}
	buffer := make([]uint16, windowsVolumePathBufferLength)
	result, _, callErr := getVolumePathNameW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
	)
	if result == 0 {
		if callErr == syscall.Errno(0) {
			callErr = syscall.EINVAL
		}
		return "", fmt.Errorf("looking up recorded cwd volume path for %q: %w", path, callErr)
	}
	volumePath := syscall.UTF16ToString(buffer)
	if volumePath == "" {
		return "", fmt.Errorf("recorded cwd volume lookup returned an empty path for %q", path)
	}
	return strings.ToLower(filepath.Clean(volumePath)), nil
}
