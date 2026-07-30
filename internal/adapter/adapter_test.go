package adapter

import (
	"context"
	"errors"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestDefaultAgentStateProvidersRegisterRevalidators(t *testing.T) {
	for _, provider := range DefaultProviders() {
		if provider.Category() != types.CategoryAgentState {
			continue
		}
		if _, ok := AgentStateRevalidatorFor(provider.Name()); !ok {
			t.Errorf("default agent-state provider %q has no registered revalidator", provider.Name())
		}
	}
}

type duplicateAgentStateProvider struct {
	tool types.Tool
}

func (p *duplicateAgentStateProvider) Name() types.Tool {
	return p.tool
}

func (p *duplicateAgentStateProvider) Category() types.Category {
	return types.CategoryAgentState
}

func (p *duplicateAgentStateProvider) Scan(context.Context, types.ScanOptions) ([]types.DebrisInfo, error) {
	return nil, nil
}

func (p *duplicateAgentStateProvider) RevalidateAgentState(context.Context, string) (types.EntryClass, error) {
	return types.EntryClassOrphaned, nil
}

func TestAgentStateRevalidatorRegistryRejectsDuplicateToolRegistration(t *testing.T) {
	const tool types.Tool = "duplicate-agent"
	registry := NewAgentStateRevalidators([]DebrisProvider{
		&duplicateAgentStateProvider{tool: tool},
		&duplicateAgentStateProvider{tool: tool},
	})

	if _, ok := registry.Lookup(tool); ok {
		t.Fatal("Lookup() accepted an ambiguous duplicate registration")
	}
	if _, err := registry.Registration(tool); !errors.Is(err, ErrAgentStateRevalidatorAmbiguous) {
		t.Fatalf("Registration() error = %v; want ErrAgentStateRevalidatorAmbiguous", err)
	}
}
