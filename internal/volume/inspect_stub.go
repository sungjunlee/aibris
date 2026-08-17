//go:build unix && !linux && !darwin

package volume

import "fmt"

// Inspect is unavailable on Unix targets without a Statfs port.
func Inspect(_ string) (Report, error) {
	return Report{}, fmt.Errorf("volume pressure is not available on this unix")
}

// PathDevice is unavailable on Unix targets without a Statfs port.
func PathDevice(path string) (string, error) {
	return pathDevice(path)
}

func pathDevice(_ string) (string, error) {
	return "", fmt.Errorf("volume device metadata unavailable")
}
