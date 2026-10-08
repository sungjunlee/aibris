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
