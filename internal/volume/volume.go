// Package volume reports host-volume pressure for scan output.
// It never prints a username-bearing mount path.
package volume

import (
	"github.com/sungjunlee/aibris/internal/types"
)

// Band is a coarse free-space class for the inspected volume.
type Band string

const (
	BandOK          Band = "ok"
	BandLow         Band = "low"
	BandCritical    Band = "critical"
	lowUsedPct           = 85.0
	criticalUsedPct      = 95.0
)

// Report is the additive scan volume object. ID is a filesystem-type plus
// device token, never a mount path.
type Report struct {
	Role                   string  `json:"role"`
	FSType                 string  `json:"fs_type"`
	ID                     string  `json:"id"`
	TotalBytes             uint64  `json:"total_bytes"`
	UsedBytes              uint64  `json:"used_bytes"`
	AvailableBytes         uint64  `json:"available_bytes"`
	UsedPercent            float64 `json:"used_percent"`
	Band                   Band    `json:"band"`
	DebrisBytes            int64   `json:"debris_bytes"`
	OtherVolumeDebrisBytes int64   `json:"other_volume_debris_bytes,omitempty"`
}

// ClassifyBand maps used% to the documented coarse bands.
func ClassifyBand(usedPercent float64) Band {
	switch {
	case usedPercent >= criticalUsedPct:
		return BandCritical
	case usedPercent >= lowUsedPct:
		return BandLow
	default:
		return BandOK
	}
}

// HumanWord is the operator-facing band label. JSON keeps BandLow as "low"
// because that value already shipped in 0.x scan --json.
func HumanWord(b Band) string {
	if b == BandLow {
		return "tight"
	}
	return string(b)
}

// SplitDebris attributes item sizes to the inspected volume versus others.
// Items whose volume cannot be compared stay on-volume so a failed lookup
// never hides debris from the home-volume figure.
func SplitDebris(rootDevice string, items []types.DebrisInfo) (onVolume, otherVolume int64) {
	if rootDevice == "" {
		for _, item := range items {
			onVolume += item.Size
		}
		return onVolume, 0
	}
	for _, item := range items {
		dev, err := pathDevice(item.Path)
		if err != nil || dev == "" || dev == rootDevice {
			onVolume += item.Size
			continue
		}
		otherVolume += item.Size
	}
	return onVolume, otherVolume
}

func usedPercent(total, available uint64) float64 {
	if total == 0 {
		return 0
	}
	used := total - available
	if available > total {
		used = 0
	}
	return (float64(used) / float64(total)) * 100
}
