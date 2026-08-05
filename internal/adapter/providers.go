package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

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

func DefaultAgentStateProviders() []DebrisProvider {
	var providers []DebrisProvider
	for _, provider := range defaultProviders {
		if provider.Category() == types.CategoryAgentState {
			providers = append(providers, provider)
		}
	}
	return providers
}

// AgentStateStoreRoots is the single source of truth for the store roots
// that agent-state providers scan. The cleanup refresh-memo fingerprint (in
// package cmd) and every agent-state store adapter must reference it, so a
// newly added agent-state root automatically flows to the fingerprint too.
func AgentStateStoreRoots() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(home, ".claude", "projects"),
		filepath.Join(home, ".cursor", "projects"),
	}, nil
}

// agentStateStoreRootFor selects the registered store root whose path ends
// with suffix.
func agentStateStoreRootFor(suffix string) (string, error) {
	roots, err := AgentStateStoreRoots()
	if err != nil {
		return "", err
	}
	for _, root := range roots {
		if strings.HasSuffix(root, suffix) {
			return root, nil
		}
	}
	return "", fmt.Errorf("agent-state store root %q is not registered", suffix)
}

// DefaultProviderIdentity identifies the concrete provider membership in the
// registry. Provider ordering does not affect the identity, while duplicate
// concrete registrations remain distinct members.
func DefaultProviderIdentity() string {
	return providerIdentity(defaultProviders)
}

func providerIdentity(providers []DebrisProvider) string {
	members := make([]string, 0, len(providers))
	for _, provider := range providers {
		members = append(members, concreteProviderType(provider))
	}
	sort.Strings(members)

	sum := sha256.Sum256([]byte(strings.Join(members, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func concreteProviderType(provider DebrisProvider) string {
	providerType := reflect.TypeOf(provider)
	if providerType == nil {
		return "<nil>"
	}

	var pointerPrefix string
	for providerType.Kind() == reflect.Pointer {
		pointerPrefix += "*"
		providerType = providerType.Elem()
	}
	if providerType.Name() == "" {
		return pointerPrefix + providerType.String()
	}
	return pointerPrefix + providerType.PkgPath() + "." + providerType.Name()
}

func AgentStateRevalidatorFor(tool types.Tool) (AgentStateRevalidator, bool) {
	return defaultAgentStateRevalidators.Lookup(tool)
}

func AgentStateRevalidatorRegistrationFor(tool types.Tool) (AgentStateRevalidatorRegistration, error) {
	return defaultAgentStateRevalidators.Registration(tool)
}
