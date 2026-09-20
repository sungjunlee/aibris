//go:build darwin

package apfs

import (
	"fmt"
	"os/exec"
)

var LookPath = exec.LookPath
var RunTMUtil = func(args ...string) ([]byte, error) {
	cmd := exec.Command("tmutil", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, FormatError(args, err, out)
	}
	return out, nil
}

func List() (int, error) {
	if _, err := LookPath("tmutil"); err != nil {
		return 0, fmt.Errorf("tmutil is not available")
	}
	out, err := RunTMUtil("listlocalsnapshots", "/")
	if err != nil {
		return 0, err
	}
	return ParseCount(out), nil
}

func Thin() error {
	if _, err := LookPath("tmutil"); err != nil {
		return fmt.Errorf("tmutil is not available")
	}
	_, err := RunTMUtil("thinlocalsnapshots", "/", fmt.Sprintf("%d", PurgeBytes), Urgency)
	return err
}
