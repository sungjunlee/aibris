package scanner

import (
	"context"
	"errors"
	"sort"

	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/types"
)

// Exclusion and retention post-scan helpers: user-exclusion merge and the
// protected-content inventory. Scan orchestration stays in scanner.go.

// applyUserExclusions removes discovered items covered by user exclusion
// patterns (--exclude flags, the per-user ignore file, and repo-local
// .aibris-ignore files under the scan roots). Exclusions affect discovery
// only; they never broaden deletion authority.
func applyUserExclusions(result *types.ScanResult, opts types.ScanOptions) {
	patterns := make([]exclude.Pattern, 0, len(opts.Excludes))
	for _, raw := range opts.Excludes {
		patterns = append(patterns, exclude.Pattern{Raw: raw, Source: types.ExcludeSourceFlag})
	}
	patterns = append(patterns, exclude.IgnoreFilePatterns(opts.Roots)...)
	if len(patterns) == 0 {
		return
	}
	matcher := exclude.New(patterns, opts.Roots)
	kept := result.Worktrees[:0]
	for _, item := range result.Worktrees {
		if matcher.Match(item.Path) {
			result.ExcludedByUser++
			continue
		}
		kept = append(kept, item)
	}
	result.Worktrees = kept
	result.ExcludedScopes = matcher.Scopes()
	result.RejectedExcludes = matcher.Rejected()
}

// scanRetention inventories protected-content stores after the debris scan.
// Store-local failures degrade the retention projection to partial without
// affecting debris results or cleanup authorization.
func scanRetention(
	ctx context.Context,
	opts types.ScanOptions,
	providers []types.RetentionProvider,
) types.RetentionProjection {
	projection := types.RetentionProjection{
		Buckets:        []types.RetentionBucket{},
		ProviderErrors: []types.RetentionProviderError{},
	}
	seen := make(map[string]bool)
	for _, provider := range providers {
		if err := ctx.Err(); err != nil {
			break
		}
		providerProjection, err := provider.Scan(ctx, opts)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				projection.Partial = true
				break
			}
			projection.Partial = true
			projection.ProviderErrors = append(projection.ProviderErrors, types.RetentionProviderError{
				StoreID: provider.Name(),
				Message: "provider failure",
			})
			continue
		}
		if providerProjection.Partial || len(providerProjection.ProviderErrors) > 0 {
			projection.Partial = true
		}
		projection.ProviderErrors = append(projection.ProviderErrors, providerProjection.ProviderErrors...)
		for _, bucket := range providerProjection.Buckets {
			key := string(bucket.StoreID) + "\x00" + bucket.BucketID
			if seen[key] {
				projection.Partial = true
				projection.ProviderErrors = append(projection.ProviderErrors, types.RetentionProviderError{
					StoreID: provider.Name(),
					Message: "duplicate retention bucket",
				})
				continue
			}
			seen[key] = true
			projection.Buckets = append(projection.Buckets, bucket)
		}
	}
	sort.Slice(projection.Buckets, func(i, j int) bool {
		if projection.Buckets[i].StoreID == projection.Buckets[j].StoreID {
			return projection.Buckets[i].BucketID < projection.Buckets[j].BucketID
		}
		return projection.Buckets[i].StoreID < projection.Buckets[j].StoreID
	})
	sort.Slice(projection.ProviderErrors, func(i, j int) bool {
		if projection.ProviderErrors[i].StoreID == projection.ProviderErrors[j].StoreID {
			return projection.ProviderErrors[i].Message < projection.ProviderErrors[j].Message
		}
		return projection.ProviderErrors[i].StoreID < projection.ProviderErrors[j].StoreID
	})
	return projection
}
