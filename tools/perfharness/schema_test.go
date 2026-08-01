package main

import (
	"testing"
)

const baseScanJSON = `{
  "worktrees": [
    {"tool":"node_modules","category":"node_modules","id":"nm","path":"/home/app/node_modules","size":4096,"mod_time":"2024-01-01T00:00:00Z"}
  ],
  "summary": {
    "total_count": 1,
    "total_size": 4096,
    "by_category": {"node_modules": {"count":1,"size":4096}},
    "by_tool": {"node_modules": {"count":1,"size":4096}}
  }
}`

// changeScanJSON has identical worktrees+summary plus an additive retention
// projection, mirroring the #139 L2 non-interference contract.
const changeScanJSON = `{
  "worktrees": [
    {"tool":"node_modules","category":"node_modules","id":"nm","path":"/home/app/node_modules","size":4096,"mod_time":"2024-01-01T00:00:00Z"}
  ],
  "summary": {
    "total_count": 1,
    "total_size": 4096,
    "by_category": {"node_modules": {"count":1,"size":4096}},
    "by_tool": {"node_modules": {"count":1,"size":4096}}
  },
  "retention": {
    "buckets": [
      {"store_id":"codex-sessions","bucket_id":"2024-03","unit_count":2,"member_count":2,"apparent_bytes":6144,"orphaned_count":1,"orphaned_bytes":4096,"selectable":false,"blocked_reason":"retention manifest preparation is not available"}
    ],
    "partial": false,
    "provider_errors": []
  }
}`

func TestParseScanOutputBaseHasNoRetention(t *testing.T) {
	res, err := parseScanOutput([]byte(baseScanJSON))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	if res.Retention != nil || res.RetentionSig != "" {
		t.Fatalf("base should have no retention; got %+v sig=%q", res.Retention, res.RetentionSig)
	}
	if res.Items != 1 || res.Bytes != 4096 {
		t.Fatalf("base scale = items %d bytes %d; want 1/4096", res.Items, res.Bytes)
	}
	if res.WorktreesSig == "" || res.SummarySig == "" || res.InventorySig == "" {
		t.Fatalf("base signatures must be non-empty")
	}
}

func TestParseScanOutputAdditiveNonInterference(t *testing.T) {
	base, err := parseScanOutput([]byte(baseScanJSON))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	change, err := parseScanOutput([]byte(changeScanJSON))
	if err != nil {
		t.Fatalf("parse change: %v", err)
	}
	// Non-interference: existing inventory is byte-identical.
	if base.WorktreesSig != change.WorktreesSig {
		t.Fatalf("worktrees changed:\nbase=%s\nchange=%s", base.WorktreesSig, change.WorktreesSig)
	}
	if base.SummarySig != change.SummarySig {
		t.Fatalf("summary changed:\nbase=%s\nchange=%s", base.SummarySig, change.SummarySig)
	}
	// Additive: change gains a retention projection; whole-inventory sig differs.
	if change.Retention == nil || change.RetentionSig == "" {
		t.Fatalf("change must carry a retention projection")
	}
	if len(change.Retention.Buckets) != 1 {
		t.Fatalf("change buckets = %d; want 1", len(change.Retention.Buckets))
	}
	if base.InventorySig == change.InventorySig {
		t.Fatalf("inventory sig should differ once retention is added")
	}
	b := change.Retention.Buckets[0]
	if b.BucketID != "2024-03" || b.UnitCount != 2 || b.OrphanedCount != 1 || b.OrphanedBytes != 4096 {
		t.Fatalf("unexpected bucket: %+v", b)
	}
}

func TestParseScanOutputSignatureIsOrderInsensitive(t *testing.T) {
	// Worktrees in a different emission order must sign identically.
	a := `{"worktrees":[{"path":"/a","category":"node_modules","id":"1"},{"path":"/b","category":"node_modules","id":"2"}],"summary":{"total_count":2,"total_size":10}}`
	b := `{"worktrees":[{"path":"/b","category":"node_modules","id":"2"},{"path":"/a","category":"node_modules","id":"1"}],"summary":{"total_count":2,"total_size":10}}`
	ra, err := parseScanOutput([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	rb, err := parseScanOutput([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if ra.WorktreesSig != rb.WorktreesSig {
		t.Fatalf("worktrees sig should be order-insensitive:\n%s\n%s", ra.WorktreesSig, rb.WorktreesSig)
	}
}

func TestParseScanOutputBucketsSorted(t *testing.T) {
	x := `{"summary":{},"retention":{"buckets":[{"store_id":"codex-sessions","bucket_id":"2024-05","unit_count":1},{"store_id":"codex-sessions","bucket_id":"2024-01","unit_count":2}]}}`
	y := `{"summary":{},"retention":{"buckets":[{"store_id":"codex-sessions","bucket_id":"2024-01","unit_count":2},{"store_id":"codex-sessions","bucket_id":"2024-05","unit_count":1}]}}`
	rx, err := parseScanOutput([]byte(x))
	if err != nil {
		t.Fatal(err)
	}
	ry, err := parseScanOutput([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if rx.RetentionSig != ry.RetentionSig {
		t.Fatalf("retention sig should be bucket-order-insensitive:\n%s\n%s", rx.RetentionSig, ry.RetentionSig)
	}
}
