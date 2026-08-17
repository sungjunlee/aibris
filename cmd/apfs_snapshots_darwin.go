//go:build darwin

package cmd

import (
	"fmt"
	"os/exec"
)

var lookPath = exec.LookPath
var runTMUtil = func(args ...string) ([]byte, error) {
	cmd := exec.Command("tmutil", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, formatTMUtilError(args, err, out)
	}
	return out, nil
}

func apfsListLocalSnapshots() (int, error) {
	if _, err := lookPath("tmutil"); err != nil {
		return 0, fmt.Errorf("tmutil is not available")
	}
	out, err := runTMUtil("listlocalsnapshots", "/")
	if err != nil {
		return 0, err
	}
	return parseLocalSnapshotCount(out), nil
}

func apfsThinLocalSnapshots() error {
	if _, err := lookPath("tmutil"); err != nil {
		return fmt.Errorf("tmutil is not available")
	}
	_, err := runTMUtil("thinlocalsnapshots", "/", fmt.Sprintf("%d", apfsSnapshotPurgeBytes), apfsSnapshotUrgency)
	return err
}
