//go:build !unix && !windows

package scancache

import "fmt"

func platformCleanupPathIdentity(string) (string, error) {
	return "", fmt.Errorf("file identity metadata unsupported on this platform")
}
