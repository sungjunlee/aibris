package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Version: version,
	Use:     "aibris",
	Short:   "Clean up AI coding tool debris (worktrees, caches, node_modules, logs)",
	Long: `aibris detects and removes disk debris left by AI coding tools
like Codex CLI, Claude Code, Cursor, and Windsurf.

Scans for:
  - worktrees (Codex, Claude)
  - node_modules (under scan roots, defaulting to $HOME)
  - build caches (Go, Xcode, Gradle, npm, Cargo)
  - pip/uv caches
  - AI logs (Codex, Claude, Cursor — requires --risky)

Run "aibris scan" first to see what's taking space,
then "aibris clean --dry-run" to preview deletions.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(cleanCmd)
}
