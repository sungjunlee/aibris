package adapter

import (
	"context"
	"errors"
	"fmt"

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

var (
	ErrAgentStateRevalidatorMissing   = errors.New("agent-state revalidator missing")
	ErrAgentStateRevalidatorAmbiguous = errors.New("agent-state revalidator registration ambiguous")
)

// AgentStateRevalidatorRegistration binds one tool to the exact concrete
// provider capability used to revalidate its entries.
type AgentStateRevalidatorRegistration struct {
	Tool        types.Tool
	ProviderID  string
	Revalidator AgentStateRevalidator
}

// AgentStateRevalidators indexes unambiguous agent-state provider
// registrations by tool. A tool with duplicate providers is retained as an
// explicit error instead of allowing the last registration to win.
type AgentStateRevalidators struct {
	registrations map[types.Tool]AgentStateRevalidatorRegistration
	errs          map[types.Tool]error
}

// NewAgentStateRevalidators derives revalidators from provider registrations.
// Missing capabilities and duplicate tool registrations remain explicit
// lookup errors so cleanup fails closed for their items.
func NewAgentStateRevalidators(providers []DebrisProvider) AgentStateRevalidators {
	revalidators := AgentStateRevalidators{
		registrations: make(map[types.Tool]AgentStateRevalidatorRegistration),
		errs:          make(map[types.Tool]error),
	}
	counts := make(map[types.Tool]int)
	for _, provider := range providers {
		if provider.Category() != types.CategoryAgentState {
			continue
		}
		tool := provider.Name()
		counts[tool]++
		if counts[tool] > 1 {
			delete(revalidators.registrations, tool)
			revalidators.errs[tool] = fmt.Errorf("%w for tool %q", ErrAgentStateRevalidatorAmbiguous, tool)
			continue
		}
		revalidator, ok := provider.(AgentStateRevalidator)
		if !ok {
			revalidators.errs[tool] = fmt.Errorf("%w for tool %q", ErrAgentStateRevalidatorMissing, tool)
			continue
		}
		revalidators.registrations[tool] = AgentStateRevalidatorRegistration{
			Tool:        tool,
			ProviderID:  concreteProviderType(provider),
			Revalidator: revalidator,
		}
	}
	return revalidators
}

func (r AgentStateRevalidators) Lookup(tool types.Tool) (AgentStateRevalidator, bool) {
	registration, err := r.Registration(tool)
	if err != nil {
		return nil, false
	}
	return registration.Revalidator, true
}

func (r AgentStateRevalidators) Registration(tool types.Tool) (AgentStateRevalidatorRegistration, error) {
	if err := r.errs[tool]; err != nil {
		return AgentStateRevalidatorRegistration{}, err
	}
	registration, ok := r.registrations[tool]
	if !ok {
		return AgentStateRevalidatorRegistration{}, fmt.Errorf("%w for tool %q", ErrAgentStateRevalidatorMissing, tool)
	}
	return registration, nil
}
