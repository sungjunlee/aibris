package adapter

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

// DebrisProvider is implemented by each adapter to discover debris from a specific AI tool.
type DebrisProvider interface {
	Name() types.Tool
	Category() types.Category
	Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error)
}

// AgentStateRevalidator is implemented by agent-state providers that can
// re-derive an entry's cleanup-driving classification immediately before
// deletion.
type AgentStateRevalidator interface {
	RevalidateAgentState(ctx context.Context, entryPath string) (types.EntryClass, error)
}

// AgentStateRevalidators indexes registered agent-state revalidators by tool.
type AgentStateRevalidators map[types.Tool]AgentStateRevalidator

// NewAgentStateRevalidators derives revalidators from provider registrations.
// Agent-state providers without this optional capability are deliberately
// absent so cleanup can fail closed for their items.
func NewAgentStateRevalidators(providers []DebrisProvider) AgentStateRevalidators {
	revalidators := make(AgentStateRevalidators)
	for _, provider := range providers {
		if provider.Category() != types.CategoryAgentState {
			continue
		}
		revalidator, ok := provider.(AgentStateRevalidator)
		if !ok {
			continue
		}
		revalidators[provider.Name()] = revalidator
	}
	return revalidators
}

func (r AgentStateRevalidators) Lookup(tool types.Tool) (AgentStateRevalidator, bool) {
	revalidator, ok := r[tool]
	return revalidator, ok
}
