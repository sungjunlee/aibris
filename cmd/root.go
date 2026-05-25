package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "aibris",
	Short: "Clean up worktree debris left by AI coding tools",
	Long: `aibris detects and removes git worktree directories
created by AI coding tools like Codex CLI, Claude Code, and Cursor.

These tools create worktree copies for session isolation but often
leave them behind, consuming significant disk space over time.`,
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(pruneCmd)
}
