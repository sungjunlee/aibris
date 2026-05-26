package scanner

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

type mockProvider struct {
	name      types.Tool
	worktrees []types.DebrisInfo
	err       error
}

func (m *mockProvider) Name() types.Tool {
	return m.name
}

func (m *mockProvider) Category() types.Category {
	return types.CategoryWorktree
}

func (m *mockProvider) Scan(_ context.Context) ([]types.DebrisInfo, error) {
	return m.worktrees, m.err
}

func TestScan_NoResults(t *testing.T) {
	s := New([]adapter.DebrisProvider{})
	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 0 {
		t.Errorf("TotalCount = %d; want 0", result.TotalCount)
	}
	if result.TotalSize != 0 {
		t.Errorf("TotalSize = %d; want 0", result.TotalSize)
	}
}

func TestScan_SingleProvider(t *testing.T) {
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name: types.ToolCodex,
			worktrees: []types.DebrisInfo{
				{ID: "a", Tool: types.ToolCodex, Size: 100},
			},
		},
	})

	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 1 {
		t.Errorf("TotalCount = %d; want 1", result.TotalCount)
	}
	if result.TotalSize != 100 {
		t.Errorf("TotalSize = %d; want 100", result.TotalSize)
	}
}

func TestScan_MultipleProviders(t *testing.T) {
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name: types.ToolCodex,
			worktrees: []types.DebrisInfo{
				{ID: "a", Size: 100},
				{ID: "b", Size: 200},
			},
		},
		&mockProvider{
			name: types.ToolClaude,
			worktrees: []types.DebrisInfo{
				{ID: "c", Size: 300},
			},
		},
	})

	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalCount != 3 {
		t.Errorf("TotalCount = %d; want 3", result.TotalCount)
	}
	if result.TotalSize != 600 {
		t.Errorf("TotalSize = %d; want 600", result.TotalSize)
	}
}

func TestScan_SortedBySizeDesc(t *testing.T) {
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name: types.ToolCodex,
			worktrees: []types.DebrisInfo{
				{ID: "small", Size: 10},
				{ID: "large", Size: 1000},
			},
		},
		&mockProvider{
			name: types.ToolClaude,
			worktrees: []types.DebrisInfo{
				{ID: "medium", Size: 100},
			},
		},
	})

	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worktrees) != 3 {
		t.Fatalf("got %d; want 3", len(result.Worktrees))
	}
	ids := []string{
		result.Worktrees[0].ID,
		result.Worktrees[1].ID,
		result.Worktrees[2].ID,
	}
	if ids[0] != "large" || ids[1] != "medium" || ids[2] != "small" {
		t.Errorf("order = %v; want [large medium small]", ids)
	}
}

func TestScan_ProviderError(t *testing.T) {
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w

	s := New([]adapter.DebrisProvider{
		&mockProvider{name: types.ToolCodex, err: errors.New("boom")},
		&mockProvider{
			name: types.ToolClaude,
			worktrees: []types.DebrisInfo{{ID: "ok", Size: 50}},
		},
	})

	result, err := s.Scan(context.Background())
	w.Close()
	stderr, _ := io.ReadAll(r)
	os.Stderr = oldStderr

	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stderr), "scan:codex:boom") {
		t.Errorf("stderr = %q; want scan:codex:boom", string(stderr))
	}
	if result.TotalCount != 1 {
		t.Errorf("TotalCount = %d; want 1", result.TotalCount)
	}
}

func TestScan_ContextCancelOnEntry(t *testing.T) {
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name: types.ToolCodex,
			worktrees: []types.DebrisInfo{{ID: "a", Size: 100}},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.Scan(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestScan_ProviderContextCancel(t *testing.T) {
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name: types.ToolCodex,
			err:  context.Canceled,
		},
	})

	_, err := s.Scan(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestScan_Default(t *testing.T) {
	result, err := Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestNew_NilProviders(t *testing.T) {
	s := New(nil)
	if s.Providers != nil {
		t.Errorf("Providers = %v; want nil", s.Providers)
	}
}
