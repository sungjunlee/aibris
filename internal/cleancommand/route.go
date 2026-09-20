package cleancommand

import (
	"github.com/spf13/cobra"
)

// Route represents the execution path for the clean command.
type Route string

const (
	RouteAPFS  Route = "apfs-snapshots"
	RouteStrip Route = "strip"
	RouteJSON  Route = "json"
	RouteScan  Route = "scan"
)

// RouteInput contains flags needed to determine the clean command route.
type RouteInput struct {
	IncludePaths   bool
	ReceiptFile    string
	DryRun         bool
	Guide          bool
	NoGuide        bool
	Strip          bool
	APFSSnapshots  bool
	JSON           bool
	Interactive    bool
	Force          bool
}

// ErrClassicRouteReceiptFile is the error message when --receipt-file is used on classic route.
const ErrClassicRouteReceiptFile = "error: --receipt-file is not available on the classic route; use --json for a classic receipt"

// errClassicRouteReceiptFile is shared by the pre-scan flag check and the
// post-scan route check so both refusals read identically.
const errClassicRouteReceiptFile = ErrClassicRouteReceiptFile

// SelectRoute determines which clean command execution path to take based on flags.
func SelectRoute(input RouteInput, apfsConflictCheck func(*cobra.Command) string, cmd *cobra.Command) (Route, string) {
	if input.IncludePaths && !input.JSON && input.ReceiptFile == "" {
		return "", "error: --include-paths requires --json"
	}
	if input.ReceiptFile != "" && input.DryRun {
		return "", "error: --receipt-file requires an execution run (remove --dry-run)"
	}
	if input.Guide && input.NoGuide {
		return "", "error: cannot use --guide with --no-guide"
	}
	if input.Strip && input.APFSSnapshots {
		return "", "error: --strip cannot be combined with --apfs-snapshots"
	}
	if input.APFSSnapshots {
		if err := apfsConflictCheck(cmd); err != "" {
			return "", err
		}
		return RouteAPFS, ""
	}
	if input.Strip {
		if input.JSON || input.Interactive || input.Guide || input.ReceiptFile != "" {
			return "", "error: --strip cannot be combined with --json, --interactive, --guide, or --receipt-file"
		}
		return RouteStrip, ""
	}
	if input.ReceiptFile != "" && input.NoGuide && !input.JSON {
		return "", errClassicRouteReceiptFile
	}
	if input.JSON {
		if !input.DryRun && input.Guide {
			return "", "error: non-dry-run --json cannot use --guide"
		}
		if !input.DryRun && !input.Force && !input.Interactive {
			return "", "error: non-dry-run --json requires --force or --interactive"
		}
		return RouteJSON, ""
	}
	return RouteScan, ""
}

// ScanSelector returns the scan mode selector string for cleanup.
func ScanSelector(strip, pressure bool) string {
	if strip {
		return "strip"
	}
	if pressure {
		return "pressure"
	}
	return "delete"
}
