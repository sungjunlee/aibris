//go:build linux || darwin

package volume

import (
	"crypto/sha256"
	"fmt"
	"os"
	"syscall"
)

// Inspect reports free space for the volume that contains path.
func Inspect(path string) (Report, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Report{}, err
	}
	bsize := uint64(st.Bsize)
	if bsize == 0 {
		return Report{}, fmt.Errorf("volume block size unavailable")
	}
	total := uint64(st.Blocks) * bsize
	available := uint64(st.Bavail) * bsize
	used := uint64(0)
	if total > available {
		used = total - available
	}
	pct := usedPercent(total, available)
	fsType := unixFSType(st)
	dev, err := pathDevice(path)
	if err != nil {
		return Report{}, err
	}
	return Report{
		FSType:         fsType,
		ID:             volumeID(fsType, dev),
		TotalBytes:     total,
		UsedBytes:      used,
		AvailableBytes: available,
		UsedPercent:    pct,
		Band:           ClassifyBand(pct),
	}, nil
}

// PathDevice is the opaque device token for path. It is not a mount path.
func PathDevice(path string) (string, error) {
	return pathDevice(path)
}

func pathDevice(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("volume device metadata unavailable")
	}
	return fmt.Sprintf("device:%d", uint64(stat.Dev)), nil
}

func volumeID(fsType, device string) string {
	sum := sha256.Sum256([]byte(fsType + "\x00" + device))
	return fsType + "-" + fmt.Sprintf("%x", sum[:4])
}
