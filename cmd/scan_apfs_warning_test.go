package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/volume"
)

func TestAddScanAPFSSnapshotWarningRequiresDarwinPressureAndSnapshots(t *testing.T) {
	tests := []struct {
		name      string
		goos      string
		volume    *volume.Report
		count     int
		listErr   error
		wantCount int
		wantCalls int
	}{
		{name: "critical", goos: "darwin", volume: &volume.Report{Band: volume.BandCritical}, count: 3, wantCount: 3, wantCalls: 1},
		{name: "tight", goos: "darwin", volume: &volume.Report{Band: volume.BandLow}, count: 1, wantCount: 1, wantCalls: 1},
		{name: "volume ok", goos: "darwin", volume: &volume.Report{Band: volume.BandOK}, count: 3},
		{name: "volume unavailable", goos: "darwin", count: 3},
		{name: "no snapshots", goos: "darwin", volume: &volume.Report{Band: volume.BandCritical}, wantCalls: 1},
		{name: "list fails", goos: "darwin", volume: &volume.Report{Band: volume.BandCritical}, listErr: errors.New("tmutil failed"), wantCalls: 1},
		{name: "non darwin", goos: "linux", volume: &volume.Report{Band: volume.BandCritical}, count: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := scanreport.View{Volume: tt.volume}
			calls := 0
			addScanAPFSSnapshotWarning(&view, tt.goos, func() (int, error) {
				calls++
				return tt.count, tt.listErr
			})
			if view.LocalAPFSSnapshots != tt.wantCount {
				t.Errorf("LocalAPFSSnapshots = %d; want %d", view.LocalAPFSSnapshots, tt.wantCount)
			}
			if calls != tt.wantCalls {
				t.Errorf("snapshot list calls = %d; want %d", calls, tt.wantCalls)
			}

			var human bytes.Buffer
			scanreport.WriteNext(&human, view)
			hasHumanWarning := strings.Contains(human.String(), "aibris clean --apfs-snapshots")
			if hasHumanWarning != (tt.wantCount > 0) {
				t.Errorf("human snapshot warning present = %t; want %t:\n%s", hasHumanWarning, tt.wantCount > 0, human.String())
			}
			jsonVolume := scanreport.EncodeJSON(view).Volume
			hasJSONWarning := jsonVolume != nil && jsonVolume.LocalAPFSSnapshots > 0 && jsonVolume.SnapshotThinningRecommended
			if hasJSONWarning != (tt.wantCount > 0) {
				t.Errorf("JSON snapshot warning present = %t; want %t: %+v", hasJSONWarning, tt.wantCount > 0, jsonVolume)
			}
		})
	}
}

func TestScanAPFSSnapshotWarningMatchesHumanAndJSON(t *testing.T) {
	view := scanreport.View{
		Volume: &volume.Report{
			Role:        "home",
			FSType:      "apfs",
			Band:        volume.BandCritical,
			UsedPercent: 99,
		},
	}
	addScanAPFSSnapshotWarning(&view, "darwin", func() (int, error) { return 2, nil })

	var human bytes.Buffer
	scanreport.WriteNext(&human, view)
	for _, want := range []string{
		"aibris clean --apfs-snapshots",
		"2 local APFS snapshots will hold freed blocks until thinned",
	} {
		if !strings.Contains(human.String(), want) {
			t.Errorf("human next block missing %q:\n%s", want, human.String())
		}
	}
	for _, private := range []string{"thinlocalsnapshots", "/Users/", "2026-08-17-101530"} {
		if strings.Contains(human.String(), private) {
			t.Errorf("human next block leaked %q:\n%s", private, human.String())
		}
	}

	out := scanreport.EncodeJSON(view)
	if out.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d; want 1", out.SchemaVersion)
	}
	if out.Volume == nil || out.Volume.LocalAPFSSnapshots != 2 || !out.Volume.SnapshotThinningRecommended {
		t.Fatalf("volume snapshot warning = %+v", out.Volume)
	}

	withoutWarning := scanreport.EncodeJSON(scanreport.View{Volume: &volume.Report{Band: volume.BandCritical}})
	raw, err := json.Marshal(withoutWarning.Volume)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "local_apfs_snapshots") || strings.Contains(string(raw), "snapshot_thinning_recommended") {
		t.Fatalf("JSON emitted an absent snapshot warning: %s", raw)
	}
}
