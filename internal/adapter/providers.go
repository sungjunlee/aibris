package adapter

import "github.com/sungjunlee/aibris/internal/types"

// Every agent-state provider registered here must implement
// AgentStateRevalidator. Cleanup refuses individual agent-state items whose
// provider does not expose that capability.
var defaultProviders = []DebrisProvider{
	&NodeModulesAdapter{},
	&BuildCacheAdapter{},
	&PipCacheAdapter{},
	&CursorAdapter{},
	&ClaudeProjectAdapter{},
	&AILogsAdapter{},
	&WindsurfAdapter{},
	NewWorktreeAdapter(),
}

var defaultAgentStateRevalidators = NewAgentStateRevalidators(defaultProviders)

func DefaultProviders() []DebrisProvider {
	return append([]DebrisProvider(nil), defaultProviders...)
}

func AgentStateRevalidatorFor(tool types.Tool) (AgentStateRevalidator, bool) {
	return defaultAgentStateRevalidators.Lookup(tool)
}
