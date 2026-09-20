//go:build !darwin

package apfs

import "fmt"

func List() (int, error) {
	return 0, fmt.Errorf("APFS snapshot thinning is only available on macOS")
}

func Thin() error {
	return fmt.Errorf("APFS snapshot thinning is only available on macOS")
}
