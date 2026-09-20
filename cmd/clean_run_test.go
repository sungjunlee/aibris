package cmd

import (
	"testing"

	"github.com/sungjunlee/aibris/internal/cleancommand"
)

func TestSelectCleanCommandRouteFlagConflicts(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		want    cleancommand.Route
		wantErr string
	}{
		{
			name: "strip vs apfs-snapshots",
			setup: func() {
				cleanStrip = true
				cleanAPFSSnapshots = true
			},
			wantErr: "error: --strip cannot be combined with --apfs-snapshots",
		},
		{
			name: "guide vs no-guide",
			setup: func() {
				cleanGuide = true
				cleanNoGuide = true
			},
			wantErr: "error: cannot use --guide with --no-guide",
		},
		{
			name: "receipt-file on classic",
			setup: func() {
				cleanReceiptFile = "receipt.json"
				cleanNoGuide = true
			},
			wantErr: cleancommand.ErrClassicRouteReceiptFile,
		},
		{
			name: "include-paths requires json",
			setup: func() {
				cleanIncludePaths = true
			},
			wantErr: "error: --include-paths requires --json",
		},
		{
			name: "receipt-file with dry-run",
			setup: func() {
				cleanReceiptFile = "receipt.json"
				cleanDryRun = true
			},
			wantErr: "error: --receipt-file requires an execution run (remove --dry-run)",
		},
		{
			name: "json non-dry-run requires force or interactive",
			setup: func() {
				cleanJSON = true
			},
			wantErr: "error: non-dry-run --json requires --force or --interactive",
		},
		{
			name: "json non-dry-run cannot use guide",
			setup: func() {
				cleanJSON = true
				cleanGuide = true
			},
			wantErr: "error: non-dry-run --json cannot use --guide",
		},
		{
			name: "strip vs json interactive guide receipt-file",
			setup: func() {
				cleanStrip = true
				cleanJSON = true
			},
			wantErr: "error: --strip cannot be combined with --json, --interactive, --guide, or --receipt-file",
		},
		{
			name: "json dry-run does not require force",
			setup: func() {
				cleanJSON = true
				cleanDryRun = true
			},
			want: cleancommand.RouteJSON,
		},
		{
			name: "json force selects json route",
			setup: func() {
				cleanJSON = true
				cleanForce = true
			},
			want: cleancommand.RouteJSON,
		},
		{
			name: "json interactive selects json route",
			setup: func() {
				cleanJSON = true
				cleanInteractive = true
			},
			want: cleancommand.RouteJSON,
		},
		{
			name: "receipt-file with json stays on json route",
			setup: func() {
				cleanReceiptFile = "receipt.json"
				cleanNoGuide = true
				cleanJSON = true
				cleanForce = true
			},
			want: cleancommand.RouteJSON,
		},
		{
			name: "strip selects strip route",
			setup: func() {
				cleanStrip = true
			},
			want: cleancommand.RouteStrip,
		},
		{
			name: "apfs-snapshots selects apfs route",
			setup: func() {
				cleanAPFSSnapshots = true
			},
			want: cleancommand.RouteAPFS,
		},
		{
			name:  "default selects scan then dispatch",
			setup: func() {},
			want:  cleancommand.RouteScan,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			t.Cleanup(resetCleanFlags)
			tt.setup()

			routeInput := cleancommand.RouteInput{
				IncludePaths:  cleanIncludePaths,
				ReceiptFile:   cleanReceiptFile,
				DryRun:        cleanDryRun,
				Guide:         cleanGuide,
				NoGuide:       cleanNoGuide,
				Strip:         cleanStrip,
				APFSSnapshots: cleanAPFSSnapshots,
				JSON:          cleanJSON,
				Interactive:   cleanInteractive,
				Force:         cleanForce,
			}
			got, errMsg := cleancommand.SelectRoute(routeInput, apfsSnapshotFlagConflict, cleanCmd)
			if tt.wantErr != "" {
				if errMsg != tt.wantErr {
					t.Fatalf("err = %q; want %q", errMsg, tt.wantErr)
				}
				if got != "" {
					t.Fatalf("route = %q; want empty on conflict", got)
				}
				return
			}
			if errMsg != "" {
				t.Fatalf("unexpected err %q", errMsg)
			}
			if got != tt.want {
				t.Fatalf("route = %q; want %q", got, tt.want)
			}
		})
	}
}
