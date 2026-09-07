package cmd

import (
	"testing"
)

func TestSelectCleanCommandRouteFlagConflicts(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		want    cleanCommandRoute
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
			wantErr: errClassicRouteReceiptFile,
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
			want: cleanCommandRouteJSON,
		},
		{
			name: "json force selects json route",
			setup: func() {
				cleanJSON = true
				cleanForce = true
			},
			want: cleanCommandRouteJSON,
		},
		{
			name: "json interactive selects json route",
			setup: func() {
				cleanJSON = true
				cleanInteractive = true
			},
			want: cleanCommandRouteJSON,
		},
		{
			name: "receipt-file with json stays on json route",
			setup: func() {
				cleanReceiptFile = "receipt.json"
				cleanNoGuide = true
				cleanJSON = true
				cleanForce = true
			},
			want: cleanCommandRouteJSON,
		},
		{
			name: "strip selects strip route",
			setup: func() {
				cleanStrip = true
			},
			want: cleanCommandRouteStrip,
		},
		{
			name: "apfs-snapshots selects apfs route",
			setup: func() {
				cleanAPFSSnapshots = true
			},
			want: cleanCommandRouteAPFS,
		},
		{
			name:  "default selects scan then dispatch",
			setup: func() {},
			want:  cleanCommandRouteScan,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			t.Cleanup(resetCleanFlags)
			tt.setup()

			got, errMsg := selectCleanCommandRoute(cleanCmd)
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
