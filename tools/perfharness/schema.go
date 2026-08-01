package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// scanEnvelope mirrors the documented `aibris scan --json` top-level shape.
// Sections are kept raw so the parser is tolerant of fields that differ between
// the base binary (no retention projection) and the change binary (additive
// retention projection). See docs/JSON_SCHEMA.md.
type scanEnvelope struct {
	Worktrees      json.RawMessage `json:"worktrees"`
	Summary        json.RawMessage `json:"summary"`
	Retention      json.RawMessage `json:"retention"`
	Partial        bool            `json:"partial"`
	ProviderErrors json.RawMessage `json:"provider_errors"`
}

// retentionBucket mirrors one Codex-sessions retention bucket. Only the fields
// the drift rule compares (month row + units/members/apparent bytes + orphan
// subset) plus presentation fields are decoded.
type retentionBucket struct {
	StoreID       string `json:"store_id"`
	BucketID      string `json:"bucket_id"`
	UnitCount     int64  `json:"unit_count"`
	MemberCount   int64  `json:"member_count"`
	ApparentBytes int64  `json:"apparent_bytes"`
	OrphanedCount int64  `json:"orphaned_count"`
	OrphanedBytes int64  `json:"orphaned_bytes"`
	Selectable    bool   `json:"selectable"`
	BlockedReason string `json:"blocked_reason"`
}

type retentionProjection struct {
	Buckets        []retentionBucket `json:"buckets"`
	Partial        bool              `json:"partial"`
	ProviderErrors json.RawMessage   `json:"provider_errors"`
}

// scanResult is the decoded, canonicalized view of one scan invocation used for
// comparison and signing.
type scanResult struct {
	// Scale.
	Items int64
	Bytes int64

	// Decoded sections.
	Worktrees []map[string]any
	Summary   map[string]any
	Retention *retentionProjection

	// Canonical signatures (hex sha256). retentionSig is empty when the binary
	// emits no retention projection (the base case).
	WorktreesSig string
	SummarySig   string
	RetentionSig string
	// InventorySig signs the whole comparable output (worktrees+summary+retention).
	InventorySig string

	Partial bool
}

// canonicalJSON marshals v with sorted map keys (encoding/json sorts
// map[string]any keys) so equal logical values hash identically regardless of
// input key order.
func canonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sortWorktrees returns a copy of items sorted by their "path" field for stable
// signing independent of provider emission order.
func sortWorktrees(items []map[string]any) []map[string]any {
	out := make([]map[string]any, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		pi, _ := out[i]["path"].(string)
		pj, _ := out[j]["path"].(string)
		if pi != pj {
			return pi < pj
		}
		// Tie-break by category then id for full determinism.
		ci, _ := out[i]["category"].(string)
		cj, _ := out[j]["category"].(string)
		if ci != cj {
			return ci < cj
		}
		ii, _ := out[i]["id"].(string)
		ij, _ := out[j]["id"].(string)
		return ii < ij
	})
	return out
}

// sortBuckets returns buckets sorted by (store_id, bucket_id).
func sortBuckets(buckets []retentionBucket) []retentionBucket {
	out := make([]retentionBucket, len(buckets))
	copy(out, buckets)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StoreID != out[j].StoreID {
			return out[i].StoreID < out[j].StoreID
		}
		return out[i].BucketID < out[j].BucketID
	})
	return out
}

// parseScanOutput decodes one `scan --json` document into a scanResult with
// canonical signatures.
func parseScanOutput(data []byte) (*scanResult, error) {
	var env scanEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decoding scan envelope: %w", err)
	}
	res := &scanResult{Partial: env.Partial}

	if len(env.Worktrees) > 0 {
		if err := json.Unmarshal(env.Worktrees, &res.Worktrees); err != nil {
			return nil, fmt.Errorf("decoding worktrees: %w", err)
		}
	}
	res.Worktrees = sortWorktrees(res.Worktrees)
	wb, err := canonicalJSON(res.Worktrees)
	if err != nil {
		return nil, err
	}
	res.WorktreesSig = hashHex(wb)

	if len(env.Summary) > 0 {
		if err := json.Unmarshal(env.Summary, &res.Summary); err != nil {
			return nil, fmt.Errorf("decoding summary: %w", err)
		}
	}
	if res.Summary == nil {
		res.Summary = map[string]any{}
	}
	res.Items = asInt64(res.Summary["total_count"])
	res.Bytes = asInt64(res.Summary["total_size"])
	sb, err := canonicalJSON(res.Summary)
	if err != nil {
		return nil, err
	}
	res.SummarySig = hashHex(sb)

	if len(env.Retention) > 0 && string(env.Retention) != "null" {
		var proj retentionProjection
		if err := json.Unmarshal(env.Retention, &proj); err != nil {
			return nil, fmt.Errorf("decoding retention: %w", err)
		}
		proj.Buckets = sortBuckets(proj.Buckets)
		res.Retention = &proj
		// The retention signature covers only the compared bucket quantities
		// (month row + units/members/apparent bytes + orphan subset), excluding
		// the volatile provider_errors raw bytes.
		rb, err := canonicalJSON(proj.Buckets)
		if err != nil {
			return nil, err
		}
		res.RetentionSig = hashHex(rb)
	}

	res.InventorySig = hashHex([]byte(res.WorktreesSig + "|" + res.SummarySig + "|" + res.RetentionSig))
	return res, nil
}

// asInt64 coerces a JSON-decoded number (float64) or json.Number to int64.
func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}
