package types

import (
	"testing"
	"time"
)

func TestCategory_IsRisky(t *testing.T) {
	tests := []struct {
		name string
		cat  Category
		want bool
	}{
		{"worktree is safe", CategoryWorktree, false},
		{"node_modules is safe", CategoryNodeModules, false},
		{"build-cache is safe", CategoryBuildCache, false},
		{"other-cache is safe", CategoryOtherCache, false},
		{"ai-logs is risky", CategoryAILogs, true},
		{"empty string is safe (backward compat)", "", false},
		{"unknown category is risky", Category("unknown-foo"), true},
		{"cursor-logs is risky", Category("cursor-logs"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cat.IsRisky()
			if got != tt.want {
				t.Errorf("IsRisky(%q) = %v; want %v", tt.cat, got, tt.want)
			}
		})
	}
}

func TestToolConstants(t *testing.T) {
	tests := []struct {
		tool Tool
		want string
	}{
		{ToolCodex, "codex"},
		{ToolClaude, "claude"},
		{ToolCursor, "cursor"},
		{ToolWindsurf, "windsurf"},
		{ToolNodeModules, "node_modules"},
		{ToolBuildCache, "build-cache"},
		{ToolPipCache, "pip-cache"},
		{ToolAILogs, "ai-logs"},
	}
	for _, tt := range tests {
		if string(tt.tool) != tt.want {
			t.Errorf("Tool %q = %q; want %q", tt.tool, string(tt.tool), tt.want)
		}
	}
}

func TestCategoryConstants(t *testing.T) {
	tests := []struct {
		cat  Category
		want string
	}{
		{CategoryWorktree, "worktree"},
		{CategoryNodeModules, "node_modules"},
		{CategoryBuildCache, "build-cache"},
		{CategoryOtherCache, "other-cache"},
		{CategoryAILogs, "ai-logs"},
	}
	for _, tt := range tests {
		if string(tt.cat) != tt.want {
			t.Errorf("Category %q = %q; want %q", tt.cat, string(tt.cat), tt.want)
		}
	}
}

func TestDebrisInfo_ZeroValue(t *testing.T) {
	w := DebrisInfo{}
	if w.Tool != "" {
		t.Errorf("zero Tool = %q; want empty", w.Tool)
	}
	if w.Category != "" {
		t.Errorf("zero Category = %q; want empty", w.Category)
	}
	if w.Size != 0 {
		t.Errorf("zero Size = %d; want 0", w.Size)
	}
	if !w.ModTime.IsZero() {
		t.Errorf("zero ModTime = %v; want zero", w.ModTime)
	}
}

func TestScanResult_Empty(t *testing.T) {
	r := &ScanResult{
		ByCategory: make(map[Category]CategorySummary),
		ByTool:     make(map[Tool]ToolSummary),
	}
	if r.TotalCount != 0 {
		t.Errorf("TotalCount = %d; want 0", r.TotalCount)
	}
	if r.TotalSize != 0 {
		t.Errorf("TotalSize = %d; want 0", r.TotalSize)
	}
	if len(r.Worktrees) != 0 {
		t.Errorf("Worktrees = %d; want 0", len(r.Worktrees))
	}
	if r.Partial() {
		t.Error("Partial() = true; want complete empty scan")
	}
	r.ProviderErrors = []ScanProviderError{{Tool: ToolCodex, Message: "boom"}}
	if !r.Partial() {
		t.Error("Partial() = false; want provider error to mark scan partial")
	}
}

func TestPruneOptions_Defaults(t *testing.T) {
	opts := PruneOptions{}
	if opts.Age != 0 {
		t.Errorf("Age = %v; want 0", opts.Age)
	}
	if opts.DryRun {
		t.Errorf("DryRun = true; want false")
	}
	if opts.Interactive {
		t.Errorf("Interactive = true; want false")
	}
	if opts.Risky {
		t.Errorf("Risky = true; want false")
	}
	if opts.Force {
		t.Errorf("Force = true; want false")
	}
}

func TestDebrisInfo_Fields(t *testing.T) {
	now := time.Now()
	w := DebrisInfo{
		Tool:     ToolCodex,
		Category: CategoryWorktree,
		ID:       "abc123",
		Project:  "my-proj",
		Path:     "/tmp/worktrees/abc123",
		Size:     4096,
		ModTime:  now,
	}
	if w.Tool != ToolCodex {
		t.Errorf("Tool = %q; want %q", w.Tool, ToolCodex)
	}
	if w.ID != "abc123" {
		t.Errorf("ID = %q; want abc123", w.ID)
	}
	if w.Size != 4096 {
		t.Errorf("Size = %d; want 4096", w.Size)
	}
	if w.ModTime != now {
		t.Errorf("ModTime = %v; want %v", w.ModTime, now)
	}
}
