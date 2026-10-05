package cmd

import (
	"testing"

	"github.com/sungjunlee/aibris/internal/volume"
)

func TestJSONVolumeBandStaysLow(t *testing.T) {
	report := volume.Report{Band: volume.BandLow, Role: "home"}
	got := jsonVolumeFromReport(report)
	if got.Band != "low" {
		t.Fatalf("JSON band = %q; want low", got.Band)
	}
	if volume.HumanWord(volume.BandLow) != "tight" {
		t.Fatalf("human word for low = %q; want tight", volume.HumanWord(volume.BandLow))
	}
}
