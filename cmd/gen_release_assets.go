package cmd

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra/doc"
)

// GenerateReleaseAssets writes shell completion scripts and man pages into
// outDir (completions/ and man/ subdirectories). It is used by the release
// pipeline so generated artifacts are reproducible from source: file names
// are fixed and the man page date is pinned.
func GenerateReleaseAssets(outDir string) error {
	completionsDir := filepath.Join(outDir, "completions")
	manDir := filepath.Join(outDir, "man")
	for _, dir := range []string{completionsDir, manDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	if err := generateAsset(filepath.Join(completionsDir, "aibris.bash"), func(w io.Writer) error {
		return rootCmd.GenBashCompletionV2(w, true)
	}); err != nil {
		return err
	}
	if err := generateAsset(filepath.Join(completionsDir, "aibris.zsh"), rootCmd.GenZshCompletion); err != nil {
		return err
	}
	if err := generateAsset(filepath.Join(completionsDir, "aibris.fish"), func(w io.Writer) error {
		return rootCmd.GenFishCompletion(w, true)
	}); err != nil {
		return err
	}
	if err := generateAsset(filepath.Join(completionsDir, "aibris.ps1"), rootCmd.GenPowerShellCompletion); err != nil {
		return err
	}

	// Pin the date so the man pages are byte-for-byte reproducible.
	date := time.Unix(0, 0).UTC()
	header := &doc.GenManHeader{
		Title:   "aibris",
		Section: "1",
		Date:    &date,
		Manual:  "aibris manual",
		Source:  "aibris",
	}
	return doc.GenManTree(rootCmd, header, manDir)
}

func generateAsset(path string, generate func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return generate(f)
}
