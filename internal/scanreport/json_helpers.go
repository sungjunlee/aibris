package scanreport

import (
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

// Encode-only JSON projection helpers. WriteJSON stays in json.go.

// EncodeJSON projects View onto the public JSON schema. Callers that need the
// encode-only struct (tests) should use this rather than rebuilding fields.
func EncodeJSON(view View) JSONOutput {
	items := make([]JSONItem, len(view.Items))
	out := JSONOutput{
		SchemaVersion: JSONSchemaVersion,
		Worktrees:     items,
		Items:         items,
		Partial:       view.Partial,
		Retention: JSONRetention{
			Buckets:        make([]JSONRetentionBucket, len(view.Retention.Buckets)),
			Partial:        view.Retention.Partial,
			ProviderErrors: make([]JSONRetentionProviderError, len(view.Retention.ProviderErrors)),
		},
		Summary: JSONSummary{
			TotalCount:           view.TotalCount,
			TotalSize:            view.TotalSize,
			PhysicalUnitCount:    view.PhysicalUnitCount,
			PhysicalTotalBytes:   view.PhysicalTotalBytes,
			TotalStrippableBytes: view.TotalStrippableBytes,
			ByCategory:           make(map[string]JSONSummaryEntry, len(view.ByCategory)),
			ByTool:               make(map[string]JSONSummaryEntry, len(view.ByTool)),
		},
	}
	for _, providerErr := range view.ProviderErrors {
		out.ProviderErrors = append(out.ProviderErrors, JSONProviderError{
			Tool:    string(providerErr.Tool),
			Message: providerErr.Message,
		})
	}
	for _, diagnostic := range view.Diagnostics {
		out.Diagnostics = append(out.Diagnostics, JSONProviderDiagnostic{
			Tool:       string(diagnostic.Tool),
			State:      string(diagnostic.State),
			Count:      diagnostic.Count,
			Bytes:      diagnostic.Bytes,
			DurationMS: diagnostic.Duration.Milliseconds(),
			Error:      diagnostic.Err,
		})
	}
	out.Exclusions = JSONExclusionsFrom(view.ExcludedByUser, view.ExcludedScopes, view.RejectedExcludes)
	for i, it := range view.Items {
		items[i] = JSONItem{
			Tool:             string(it.Tool),
			Category:         string(it.Category),
			ID:               it.ID,
			Project:          it.Project,
			Source:           it.Source,
			Path:             it.Path,
			Size:             it.Size,
			ModTime:          it.ModTime.Format(time.RFC3339),
			Status:           string(it.Status),
			Classification:   string(it.Classification),
			Risk:             it.Risk,
			Reason:           it.Reason,
			CleanupKind:      string(it.CleanupKind),
			CleanupCommand:   it.CleanupCommand,
			PhysicalTargetID: it.PhysicalTargetID,
			StrippableBytes:  it.StrippableBytes,
			StrippablePaths:  it.StrippablePaths,
		}
	}
	for i, bucket := range view.Retention.Buckets {
		out.Retention.Buckets[i] = JSONRetentionBucket{
			StoreID:       string(bucket.StoreID),
			BucketID:      bucket.BucketID,
			UnitCount:     bucket.UnitCount,
			MemberCount:   bucket.MemberCount,
			ApparentBytes: bucket.ApparentBytes,
			OrphanedCount: bucket.OrphanedCount,
			OrphanedBytes: bucket.OrphanedBytes,
		}
	}
	for i, providerErr := range view.Retention.ProviderErrors {
		out.Retention.ProviderErrors[i] = JSONRetentionProviderError{
			StoreID: string(providerErr.StoreID),
			Message: providerErr.Message,
		}
	}
	if view.Volume != nil {
		out.Volume = JSONVolumeFromReport(*view.Volume)
	}
	for cat, s := range view.ByCategory {
		out.Summary.ByCategory[string(cat)] = JSONSummaryEntry{
			Count:              s.Count,
			Size:               s.Size,
			PhysicalUnitCount:  s.PhysicalUnitCount,
			PhysicalTotalBytes: s.PhysicalTotalBytes,
			StrippableBytes:    s.StrippableBytes,
		}
	}
	for tool, s := range view.ByTool {
		out.Summary.ByTool[string(tool)] = JSONSummaryEntry{
			Count:              s.Count,
			Size:               s.Size,
			PhysicalUnitCount:  s.PhysicalUnitCount,
			PhysicalTotalBytes: s.PhysicalTotalBytes,
			StrippableBytes:    s.StrippableBytes,
		}
	}
	applyPhysicalJSONSummary(&out, view.sourceDebris())
	return out
}

func applyPhysicalJSONSummary(out *JSONOutput, items []types.DebrisInfo) {
	units := cleaner.PhysicalInventory(items)
	out.Summary.PhysicalUnitCount = len(units)
	var total int64
	for _, unit := range units {
		total += unit.Size
	}
	out.Summary.PhysicalTotalBytes = total
}

// JSONVolumeFromReport is the encode-only volume object. Band stays the JSON
// token (low/critical), not the human word.
func JSONVolumeFromReport(report volume.Report) *JSONVolume {
	return &JSONVolume{
		Role:                   report.Role,
		FSType:                 report.FSType,
		ID:                     report.ID,
		TotalBytes:             report.TotalBytes,
		UsedBytes:              report.UsedBytes,
		AvailableBytes:         report.AvailableBytes,
		UsedPercent:            report.UsedPercent,
		Band:                   string(report.Band),
		DebrisBytes:            report.DebrisBytes,
		OtherVolumeDebrisBytes: report.OtherVolumeDebrisBytes,
	}
}

// JSONExclusionsFrom is the encode-only exclusions object shared by scan JSON
// and clean JSON. It is nil when no exclusion configuration was honored or
// rejected, so schema_version stays 1.
func JSONExclusionsFrom(excludedByUser int, scopes []types.ExcludedScope, rejected []types.RejectedExclude) *JSONExclusions {
	if excludedByUser == 0 && len(scopes) == 0 && len(rejected) == 0 {
		return nil
	}
	out := &JSONExclusions{
		ExcludedCount: excludedByUser,
		Scopes:        make([]JSONExcludedScope, 0, len(scopes)),
		Rejected:      make([]JSONRejectedExclude, 0, len(rejected)),
	}
	for _, scope := range scopes {
		out.Scopes = append(out.Scopes, JSONExcludedScope{
			Pattern:  scope.Pattern,
			Resolved: scope.Resolved,
			Source:   string(scope.Source),
			Count:    scope.Count,
		})
	}
	for _, item := range rejected {
		out.Rejected = append(out.Rejected, JSONRejectedExclude{
			Pattern: item.Pattern,
			Source:  string(item.Source),
			Reason:  item.Reason,
		})
	}
	return out
}

// JSONExclusionsFromResult projects ScanResult exclusion diagnostics. Nil
// results and scans without exclusion configuration omit the object.
func JSONExclusionsFromResult(result *types.ScanResult) *JSONExclusions {
	if result == nil {
		return nil
	}
	return JSONExclusionsFrom(result.ExcludedByUser, result.ExcludedScopes, result.RejectedExcludes)
}

// JSONProtectPathsFrom is the encode-only protect-path object for clean JSON.
// It is nil when no --protect-path flags were honored or rejected, so
// schema_version stays 1. Honored pins that matched zero inventory items still
// emit a scope with count 0.
func JSONProtectPathsFrom(protectedCount int, scopes []types.ExcludedScope, rejected []types.RejectedExclude) *JSONProtectPaths {
	exclusions := JSONExclusionsFrom(protectedCount, scopes, rejected)
	if exclusions == nil {
		return nil
	}
	return &JSONProtectPaths{
		ProtectedCount: exclusions.ExcludedCount,
		Scopes:         exclusions.Scopes,
		Rejected:       exclusions.Rejected,
	}
}
