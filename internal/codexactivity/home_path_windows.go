//go:build windows

package codexactivity

import (
	"os"
	"syscall"
	"unsafe"
)

// finalPathNameDOS is VOLUME_NAME_DOS|FILE_NAME_NORMALIZED. The DOS form is
// what canonicalPath returns; normalized names avoid 8.3 false mismatches.
const finalPathNameDOS = 0

var getFinalPathNameByHandleW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// homePathIsFinal reports whether canonicalPath(home) is the final DOS path.
// EvalSymlinks does not follow Windows junctions or subst drives, so an
// ancestor reparse point leaves the canonical home on the alias while Codex
// can record session cwd values on the target. A mismatch or a lookup failure
// cannot prove that no session matches.
func homePathIsFinal(home string) bool {
	raw, ok := windowsFinalPathName(home)
	if !ok {
		return false
	}
	dos, ok := dosPathFromFinal(raw)
	if !ok {
		return false
	}
	return canonicalPath(home) == canonicalPath(dos)
}

func windowsFinalPathName(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	// The API's maximum is 32,767 characters plus a terminating null.
	buf := make([]uint16, 32768)
	n, _, _ := getFinalPathNameByHandleW.Call(
		uintptr(syscall.Handle(file.Fd())),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(finalPathNameDOS),
	)
	if n == 0 || n >= uintptr(len(buf)) {
		return "", false
	}
	return syscall.UTF16ToString(buf[:n]), true
}
