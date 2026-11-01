package cmd

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
)

func TestRootHelpListsEveryAgentStateProvider(t *testing.T) {
	var want []string
	for _, provider := range adapter.DefaultAgentStateProviders() {
		want = append(want, strings.ToLower(string(provider.Name())))
	}
	if len(want) == 0 {
		t.Fatal("no agent-state providers registered")
	}
	sort.Strings(want)

	match := regexp.MustCompile(`(?m)^  - agent state \((.+) — orphaned only\)$`).FindStringSubmatch(rootCmd.Long)
	if match == nil {
		t.Fatalf("root help has no agent-state line:\n%s", rootCmd.Long)
	}
	var got []string
	for _, name := range strings.Split(match[1], ", ") {
		got = append(got, strings.ToLower(name))
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("root help agent-state list = %v, registry = %v", got, want)
	}
}

// TestRootHelpExplainsActiveWorktreeRoutes pins the public meaning of root
// help: guided review can select an active worktree, and
// --include-active-worktrees is the classic opt-in. It does not pin the
// sentence or which source line defines it.
func TestRootHelpExplainsActiveWorktreeRoutes(t *testing.T) {
	help := strings.Join(strings.Fields(rootCmd.Long), " ")
	var missing []string
	for _, want := range []string{
		"guided review",
		"classic path",
		"--include-active-worktrees",
		"hard locks",
		"safety checks",
	} {
		if !strings.Contains(help, want) {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("root help missing %q:\n%s", missing, rootCmd.Long)
	}
	if strings.Contains(help, "protected unless --include-active-worktrees") {
		t.Errorf("root help still presents --include-active-worktrees as the only active-worktree route:\n%s", rootCmd.Long)
	}
}
