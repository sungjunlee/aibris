package adapter

import (
	"context"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestEstimateDirSize_ExportedFunction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	got := EstimateDirSize(ctx, dir)
	if got != 0 {
		t.Errorf("EstimateDirSize(empty) = %d; want 0", got)
	}
}

func TestNewestTreeModTime_ExportedFunction(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	got := NewestTreeModTime(ctx, dir)
	if !got.IsZero() {
		t.Errorf("NewestTreeModTime(empty) = %v; want zero", got)
	}
}

func TestDefaultAgentStateProviders(t *testing.T) {
	providers := DefaultAgentStateProviders()
	if len(providers) == 0 {
		t.Error("DefaultAgentStateProviders() returned empty; want at least claude and cursor")
	}

	hasClaudeOrCursor := false
	for _, p := range providers {
		if p.Name() == types.ToolClaude || p.Name() == types.ToolCursor {
			hasClaudeOrCursor = true
			break
		}
	}
	if !hasClaudeOrCursor {
		t.Error("DefaultAgentStateProviders() missing claude or cursor provider")
	}
}

func TestDefaultProviderIdentity(t *testing.T) {
	id := DefaultProviderIdentity()
	if id == "" {
		t.Error("DefaultProviderIdentity() = empty; want non-empty hash")
	}
}

func TestIdentity(t *testing.T) {
	providers := DefaultProviders()
	id := Identity(providers)
	if id == "" {
		t.Error("Identity() = empty; want non-empty hash")
	}

	// Same providers should produce same identity
	id2 := Identity(providers)
	if id != id2 {
		t.Errorf("Identity() not stable: %q != %q", id, id2)
	}
}

func TestAgentStateRevalidatorRegistrationFor(t *testing.T) {
	tests := []struct {
		tool    types.Tool
		wantErr bool
	}{
		{types.ToolClaude, false},
		{types.ToolCursor, false},
		{types.ToolNodeModules, true},
		{types.ToolBuildCache, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.tool), func(t *testing.T) {
			got, err := AgentStateRevalidatorRegistrationFor(tt.tool)
			if (err != nil) != tt.wantErr {
				t.Errorf("AgentStateRevalidatorRegistrationFor(%q) error = %v; wantErr %v", tt.tool, err, tt.wantErr)
			}
			if err == nil && got.Tool != tt.tool {
				t.Errorf("registration Tool = %q; want %q", got.Tool, tt.tool)
			}
		})
	}
}

func TestAgentStateRevalidatorRegistrationFor_FunctionWorks(t *testing.T) {
	reg, err := AgentStateRevalidatorRegistrationFor(types.ToolClaude)
	if err != nil {
		t.Skipf("claude revalidator not registered: %v", err)
	}

	if reg.Tool != types.ToolClaude {
		t.Errorf("registration Tool = %q; want claude", reg.Tool)
	}

	// Verify the registration has the expected fields
	if reg.Tool == "" {
		t.Error("registration Tool is empty")
	}
}
