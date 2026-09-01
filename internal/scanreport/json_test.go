package scanreport

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestEncodeJSONPublicSchemaFixture(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				Tool:     types.ToolCodex,
				Category: types.CategoryWorktree,
				ID:       "hash1",
				Project:  "myproject",
				Source:   ".codex",
				Path:     "/home/user/.codex/worktrees/hash1",
				Size:     102400,
				ModTime:  now,
				Status:   types.WorktreeActive,
			},
			{
				Tool:           types.ToolClaude,
				Category:       types.CategoryWorktree,
				ID:             "session-42",
				Project:        "otherproj",
				Path:           "/home/user/.claude/worktrees/session-42",
				Size:           204800,
				ModTime:        now.Add(-72 * time.Hour),
				Status:         types.WorktreeOrphaned,
				CleanupKind:    types.CleanupCommand,
				CleanupCommand: []string{"go", "clean", "-cache"},
			},
		},
		TotalCount:         2,
		TotalSize:          307200,
		PhysicalUnitCount:  2,
		PhysicalTotalBytes: 307200,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 2, Size: 307200, PhysicalUnitCount: 2, PhysicalTotalBytes: 307200},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex:  {Count: 1, Size: 102400, PhysicalUnitCount: 1, PhysicalTotalBytes: 102400},
			types.ToolClaude: {Count: 1, Size: 204800, PhysicalUnitCount: 1, PhysicalTotalBytes: 204800},
		},
	}

	var buf bytes.Buffer
	WriteJSON(&buf, FromResult(r, testPolicy()))
	var out JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if out.SchemaVersion != JSONSchemaVersion {
		t.Errorf("SchemaVersion = %d; want %d", out.SchemaVersion, JSONSchemaVersion)
	}
	if out.Summary.TotalCount != 2 || out.Summary.TotalSize != 307200 {
		t.Errorf("summary totals = %d/%d; want 2/307200", out.Summary.TotalCount, out.Summary.TotalSize)
	}
	if len(out.Items) != 2 || len(out.Worktrees) != 2 {
		t.Fatalf("items=%d worktrees=%d; want 2/2", len(out.Items), len(out.Worktrees))
	}
	itemsJSON, _ := json.Marshal(out.Items)
	worktreesJSON, _ := json.Marshal(out.Worktrees)
	if !bytes.Equal(itemsJSON, worktreesJSON) {
		t.Errorf("items and worktrees differ")
	}
	w0 := out.Items[0]
	if w0.ID != "hash1" || w0.Tool != "codex" || w0.Status != "active" || w0.Risk != "low" {
		t.Errorf("item0 = %+v", w0)
	}
	if w0.ModTime != "2026-05-25T12:00:00Z" {
		t.Errorf("item0 mod_time = %q", w0.ModTime)
	}
	if !strings.Contains(w0.Reason, "protected") {
		t.Errorf("item0 reason = %q", w0.Reason)
	}
	if len(w0.CleanupCommand) != 0 {
		t.Errorf("item0 cleanup_command = %v; want empty array", w0.CleanupCommand)
	}
	w1 := out.Items[1]
	if w1.Status != "orphaned" || w1.CleanupKind != "command" {
		t.Errorf("item1 = %+v", w1)
	}
	if !strings.Contains(w1.Reason, "parent repo metadata missing") {
		t.Errorf("item1 reason = %q", w1.Reason)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["partial"]; ok {
		t.Error("successful JSON includes partial")
	}
}

func TestEncodeJSONPlainDirIsReviewOnly(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(filepath.Join(plain, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	mixed := filepath.Join(t.TempDir(), "mixed")
	if err := os.MkdirAll(mixed, 0o755); err != nil {
		t.Fatal(err)
	}
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				ID: "plain", Tool: types.ToolCodex, Category: types.CategoryWorktree,
				Status: types.WorktreePlain, Path: plain, Size: 9 << 30, ModTime: old,
				Reason:          "invalid: missing .git marker",
				StrippableBytes: 1 << 30, StrippablePaths: []string{filepath.Join(plain, "node_modules")},
			},
			{
				ID: "mixed", Tool: types.ToolUnknown, Category: types.CategoryWorktree,
				Source: "superpowers", Status: types.WorktreePlain, Path: mixed, Size: 4 << 20, ModTime: old,
				Reason: "invalid: missing .git marker",
			},
		},
		TotalCount: 2,
		TotalSize:  9<<30 + 4<<20,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 2, Size: 9<<30 + 4<<20},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex:   {Count: 1, Size: 9 << 30},
			types.ToolUnknown: {Count: 1, Size: 4 << 20},
		},
	}
	view := FromResult(r, testPolicy())
	if len(view.Items) != 2 || !view.Items[0].ReviewOnly || !view.Items[1].ReviewOnly {
		t.Fatalf("review-only flags = %+v", view.Items)
	}
	if view.ReviewOnly.Count != 2 || view.DefaultClean.EligibleCount != 0 {
		t.Fatalf("review-only=%d default-clean=%d; want 2/0", view.ReviewOnly.Count, view.DefaultClean.EligibleCount)
	}
	if len(view.ReclaimPaths) != 0 {
		t.Fatalf("plain-dir/mixed opened reclaim paths: %+v", view.ReclaimPaths)
	}

	out := EncodeJSON(view)
	if len(out.Items) != 2 {
		t.Fatalf("items = %d; want 2 review-only rows", len(out.Items))
	}
	for _, row := range out.Items {
		if row.Status != string(types.WorktreePlain) {
			t.Errorf("%s status = %q; want plain-dir", row.ID, row.Status)
		}
		if !strings.Contains(row.Reason, "invalid: missing .git marker") {
			t.Errorf("%s reason = %q", row.ID, row.Reason)
		}
	}
}

func TestEncodeJSONOmitsWorktreeClassification(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				Tool:           types.ToolClaude,
				Category:       types.CategoryAgentState,
				ID:             "encoded-project-key",
				ModTime:        now,
				Classification: types.EntryClassOrphaned,
				Reason:         "recorded cwd does not exist: /gone",
			},
			{
				Tool:     types.ToolCodex,
				Category: types.CategoryWorktree,
				ID:       "worktree-without-classification",
				ModTime:  now,
				Status:   types.WorktreeActive,
			},
		},
		ByCategory: map[types.Category]types.CategorySummary{},
		ByTool:     map[types.Tool]types.ToolSummary{},
	}
	var buf bytes.Buffer
	WriteJSON(&buf, FromResult(r, testPolicy()))
	var raw struct {
		Worktrees []map[string]json.RawMessage `json:"worktrees"`
	}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw.Worktrees[1]["classification"]; ok {
		t.Error("worktree without classification emitted classification")
	}
}
