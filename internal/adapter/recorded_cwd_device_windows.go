//go:build windows

package adapter

import "os"

// Volume-boundary detection is not implemented on Windows, so this barrier
// relies on home/temp containment alone.
var recordedCWDDeviceID = func(_ string, _ os.FileInfo) (uint64, error) {
	return 0, nil
}
