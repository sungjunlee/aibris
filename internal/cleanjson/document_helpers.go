package cleanjson

import (
	"fmt"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Document completeness, evidence, and cleanup-kind helpers.
// Build/Render/Encode stay in document.go.

func refusePartialScan(result *types.ScanResult) error {
	if result == nil || !result.Partial() {
		return nil
	}
	providers := make([]string, 0, len(result.ProviderErrors))
	for _, providerErr := range result.ProviderErrors {
		providers = append(providers, string(providerErr.Tool))
	}
	return fmt.Errorf("cleanup requires a complete scan; failed providers: %s", strings.Join(providers, ", "))
}

func evidenceFor(source Source, evidence PlanEvidence) Evidence {
	sourceName := source.Kind
	if sourceName == "" {
		sourceName = SourceLive
	}
	observedAt := evidence.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	return Evidence{
		Complete:   len(evidence.ProviderErrors) == 0,
		Source:     sourceName,
		ObservedAt: observedAt.Format(time.RFC3339Nano),
	}
}

func cleanupKind(item types.DebrisInfo) types.CleanupKind {
	if item.CleanupKind != "" {
		return item.CleanupKind
	}
	return types.CleanupRemovePath
}
