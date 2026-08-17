//go:build !darwin

package cmd

import "fmt"

func apfsListLocalSnapshots() (int, error) {
	return 0, fmt.Errorf("APFS snapshot thinning is only available on macOS")
}

func apfsThinLocalSnapshots() error {
	return fmt.Errorf("APFS snapshot thinning is only available on macOS")
}
