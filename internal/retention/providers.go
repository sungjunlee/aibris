package retention

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

var defaultProviders = []types.RetentionProvider{
	NewCodexSessionsProvider(),
}

// DefaultProviders returns the registered read-only retention providers.
func DefaultProviders() []types.RetentionProvider {
	return append([]types.RetentionProvider(nil), defaultProviders...)
}

// DefaultProviderIdentity identifies the sorted concrete retention-provider
// membership so the last-scan cache can detect provider additions, removals,
// and duplicate registrations. It does not detect behavior changes inside an
// unchanged provider; those require a cache revision bump.
func DefaultProviderIdentity() string {
	members := make([]string, 0, len(defaultProviders))
	for _, provider := range defaultProviders {
		members = append(members, string(provider.Name()))
	}
	sort.Strings(members)
	sum := sha256.Sum256([]byte(strings.Join(members, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
