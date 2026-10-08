package cmd

import (
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
)

func TestRootHelpListsEveryAgentStateProvider(t *testing.T) {
	providers := adapter.DefaultAgentStateProviders()
	if len(providers) == 0 {
		t.Fatal("no agent-state providers registered")
	}
	help := strings.ToLower(rootCmd.Long)
	for _, provider := range providers {
		if !strings.Contains(help, string(provider.Name())) {
			t.Errorf("root help omits agent-state provider %q", provider.Name())
		}
	}
}
