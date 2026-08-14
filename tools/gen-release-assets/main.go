// Command gen-release-assets generates the shell completion scripts and man
// pages that are packaged in release archives.
package main

import (
	"fmt"
	"os"

	"github.com/sungjunlee/aibris/cmd"
)

func main() {
	outDir := "release-assets"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	if err := cmd.GenerateReleaseAssets(outDir); err != nil {
		fmt.Fprintf(os.Stderr, "gen-release-assets: %v\n", err)
		os.Exit(1)
	}
}
