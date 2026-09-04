package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

func scanForClean(ctx context.Context, roots, excludes []string, explicit bool) (*types.ScanResult, scanSource, error) {
	return loadLastScanSession(ctx, roots, excludes, cleanScanSelector(), explicit, true)
}

func scanForCleanQuiet(ctx context.Context, roots, excludes []string, explicit bool) (*types.ScanResult, scanSource, error) {
	return loadLastScanSession(ctx, roots, excludes, cleanScanSelector(), explicit, false)
}

func cleanScanSelector() string {
	if cleanStrip {
		return "strip"
	}
	if cleanPressure {
		return "pressure"
	}
	return "delete"
}

var errIncompleteCleanupScan = errors.New("cleanup requires a complete scan")

func requireCompleteScan(result *types.ScanResult) error {
	if result == nil || !result.Partial() {
		return nil
	}
	providers := make([]string, 0, len(result.ProviderErrors))
	for _, providerErr := range result.ProviderErrors {
		providers = append(providers, string(providerErr.Tool))
	}
	return fmt.Errorf("%w; failed providers: %s", errIncompleteCleanupScan, strings.Join(providers, ", "))
}

func filterTargetsWithoutScanEvidence(targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	filtered := targets[:0]
	protections := make(map[string]cleanAuditReason)
	for _, target := range targets {
		if target.ScanPathEvidenceRequired && target.ScanPathIdentity == "" {
			protections[cleanAuditItemKey(target)] = cleanReasonScanEvidenceUnavailable
			continue
		}
		filtered = append(filtered, target)
	}
	return filtered, protections
}
