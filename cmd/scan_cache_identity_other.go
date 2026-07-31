//go:build !unix && !windows

package cmd

import "fmt"

func platformCleanupPathIdentity(string) (string, error) {
	return "", fmt.Errorf("file identity metadata unsupported on this platform")
}
