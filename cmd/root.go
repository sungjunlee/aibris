package cmd

import (
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sungjunlee/aibris/internal/adapter"
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
  - agent state (` + agentStateToolList() + ` — orphaned only)
  - AI logs (Codex, Claude, Windsurf — requires --risky)
  - protected Codex session retention aggregates (read-only inventory)

Run "aibris scan" first to see what's taking space,
then "aibris clean --dry-run" to preview deletions.

Safety gates:
  - clean previews with --dry-run and asks for confirmation before deleting
  - AI logs are only touched with --risky
  - active worktrees are protected unless --include-active-worktrees
  - scans stay under $HOME; deletions outside it are rejected

Exit status:
  0  successful completion
  1  invalid usage, fail-closed safety refusal, cancellation, or failed execution`,
}

// agentStateToolList names every registered agent-state provider, so the
// help text cannot fall behind the registry.
func agentStateToolList() string {
	var names []string
	for _, provider := range adapter.DefaultAgentStateProviders() {
		name := string(provider.Name())
		names = append(names, strings.ToUpper(name[:1])+name[1:])
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
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
