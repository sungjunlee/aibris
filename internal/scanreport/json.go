package scanreport

import (
	"encoding/json"
	"io"
)

// JSONSchemaVersion is the version of the top-level `scan --json` contract.
// Consumers should treat an unknown version as unsupported. The historical
// `worktrees` field stays as a 0.x compatibility alias for the canonical
// `items` array during the 0.x period (see docs/JSON_SCHEMA.md).
const JSONSchemaVersion = 1

// JSONItem is the encode-only projection of View.Items onto the public
// scan --json item schema. It is not a second domain model.
type JSONItem struct {
	Tool             string   `json:"tool"`
	Category         string   `json:"category"`
	ID               string   `json:"id"`
	Project          string   `json:"project"`
	Source           string   `json:"source"`
	Path             string   `json:"path"`
	Size             int64    `json:"size"`
	ModTime          string   `json:"mod_time"`
	Status           string   `json:"status"`
	Classification   string   `json:"classification,omitempty"`
	Risk             string   `json:"risk"`
	Reason           string   `json:"reason"`
	CleanupKind      string   `json:"cleanup_kind"`
	CleanupCommand   []string `json:"cleanup_command"`
	PhysicalTargetID string   `json:"physical_target_id"`
	StrippableBytes  int64    `json:"strippable_bytes,omitempty"`
	StrippablePaths  []string `json:"strippable_paths,omitempty"`
}

// JSONSummaryEntry is the encode-only by_category/by_tool summary row.
type JSONSummaryEntry struct {
	Count              int   `json:"count"`
	Size               int64 `json:"size"`
	PhysicalUnitCount  int   `json:"physical_unit_count"`
	PhysicalTotalBytes int64 `json:"physical_total_bytes"`
	StrippableBytes    int64 `json:"strippable_bytes,omitempty"`
}

// JSONSummary is the encode-only top-level summary object.
type JSONSummary struct {
	TotalCount           int                         `json:"total_count"`
	TotalSize            int64                       `json:"total_size"`
	PhysicalUnitCount    int                         `json:"physical_unit_count"`
	PhysicalTotalBytes   int64                       `json:"physical_total_bytes"`
	TotalStrippableBytes int64                       `json:"total_strippable_bytes,omitempty"`
	ByCategory           map[string]JSONSummaryEntry `json:"by_category"`
	ByTool               map[string]JSONSummaryEntry `json:"by_tool"`
}

// JSONProviderError is the encode-only partial-scan provider error row.
type JSONProviderError struct {
	Tool    string `json:"tool"`
	Message string `json:"message"`
}

// JSONProviderDiagnostic is experimental; see docs/JSON_SCHEMA.md.
type JSONProviderDiagnostic struct {
	Tool       string `json:"tool"`
	State      string `json:"state"`
	Count      int    `json:"count"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// JSONRetentionProviderError is the encode-only retention store error row.
type JSONRetentionProviderError struct {
	StoreID string `json:"store_id"`
	Message string `json:"message"`
}

// JSONRetentionBucket is the encode-only retention aggregate row.
type JSONRetentionBucket struct {
	StoreID       string `json:"store_id"`
	BucketID      string `json:"bucket_id"`
	UnitCount     int    `json:"unit_count"`
	MemberCount   int    `json:"member_count"`
	ApparentBytes int64  `json:"apparent_bytes"`
	OrphanedCount int    `json:"orphaned_count"`
	OrphanedBytes int64  `json:"orphaned_bytes"`
}

// JSONRetention is the encode-only top-level retention object.
type JSONRetention struct {
	Buckets        []JSONRetentionBucket        `json:"buckets"`
	Partial        bool                         `json:"partial"`
	ProviderErrors []JSONRetentionProviderError `json:"provider_errors"`
}

// JSONExcludedScope is the encode-only honored exclusion row.
type JSONExcludedScope struct {
	Pattern  string `json:"pattern"`
	Resolved string `json:"resolved"`
	Source   string `json:"source"`
	Count    int    `json:"count"`
}

// JSONRejectedExclude is the encode-only rejected exclusion or protect-path row.
type JSONRejectedExclude struct {
	Pattern string `json:"pattern"`
	Source  string `json:"source"`
	Reason  string `json:"reason"`
}

// JSONExclusions is the encode-only top-level exclusions object.
type JSONExclusions struct {
	ExcludedCount int                   `json:"excluded_count"`
	Scopes        []JSONExcludedScope   `json:"scopes"`
	Rejected      []JSONRejectedExclude `json:"rejected"`
}

// JSONProtectPaths is the encode-only clean-only protect-path object. It uses
// the same scopes/rejected row shape as JSONExclusions so JSON consumers can
// parse rejected pins the same way. Scan JSON never includes this object.
type JSONProtectPaths struct {
	ProtectedCount int                   `json:"protected_count"`
	Scopes         []JSONExcludedScope   `json:"scopes"`
	Rejected       []JSONRejectedExclude `json:"rejected"`
}

// JSONVolume is the encode-only home-volume pressure object.
type JSONVolume struct {
	Role                   string  `json:"role"`
	FSType                 string  `json:"fs_type"`
	ID                     string  `json:"id"`
	TotalBytes             uint64  `json:"total_bytes"`
	UsedBytes              uint64  `json:"used_bytes"`
	AvailableBytes         uint64  `json:"available_bytes"`
	UsedPercent            float64 `json:"used_percent"`
	Band                   string  `json:"band"`
	DebrisBytes            int64   `json:"debris_bytes"`
	OtherVolumeDebrisBytes int64   `json:"other_volume_debris_bytes,omitempty"`
}

// JSONOutput is the encode-only top-level scan --json document. Field names,
// types, and nesting are the public schema.
type JSONOutput struct {
	SchemaVersion  int                      `json:"schema_version"`
	Items          []JSONItem               `json:"items"`
	Worktrees      []JSONItem               `json:"worktrees"`
	Summary        JSONSummary              `json:"summary"`
	Retention      JSONRetention            `json:"retention"`
	Volume         *JSONVolume              `json:"volume,omitempty"`
	Exclusions     *JSONExclusions          `json:"exclusions,omitempty"`
	Partial        bool                     `json:"partial,omitempty"`
	ProviderErrors []JSONProviderError      `json:"provider_errors,omitempty"`
	Diagnostics    []JSONProviderDiagnostic `json:"diagnostics,omitempty"`
}

// WriteJSON encodes view as the public scan --json document.
func WriteJSON(w io.Writer, view View) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(EncodeJSON(view))
}
