//go:build windows

package adapter

import "os"

func memberSysIdentity(info os.FileInfo) string {
	return ""
}
