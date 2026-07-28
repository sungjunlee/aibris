package adapter

import (
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
