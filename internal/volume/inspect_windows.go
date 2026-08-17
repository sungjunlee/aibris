//go:build windows

package volume

import "fmt"

// Inspect is unavailable on Windows. Volume-boundary work is tracked separately.
func Inspect(_ string) (Report, error) {
	return Report{}, fmt.Errorf("volume pressure is not available on windows")
}

// PathDevice is unavailable on Windows.
func PathDevice(path string) (string, error) {
	return pathDevice(path)
}

func pathDevice(_ string) (string, error) {
	return "", fmt.Errorf("volume device metadata unavailable")
}
