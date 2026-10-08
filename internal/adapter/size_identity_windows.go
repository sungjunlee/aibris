//go:build windows

package adapter

import "os"

// Windows FileInfo does not expose device/inode identity; count per path.
func hardlinkSizeIdentity(_ os.FileInfo) (sizeFileIdentity, bool) {
	return sizeFileIdentity{}, false
}
