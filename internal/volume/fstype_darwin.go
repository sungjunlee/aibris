//go:build darwin

package volume

import (
	"strings"
	"syscall"
)

func unixFSType(st syscall.Statfs_t) string {
	var b []byte
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	name := strings.ToLower(string(b))
	if name == "" {
		return "unix"
	}
	return name
}
