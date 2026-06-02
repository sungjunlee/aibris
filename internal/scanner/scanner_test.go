package scanner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

type mockProvider struct {
	name      types.Tool
	worktrees []types.DebrisInfo
	err       error
	roots     []string
}

func (m *mockProvider) Name() types.Tool {
	return m.name
}

func (m *mockProvider) Category() types.Category {
	return types.CategoryWorktree
}

func (m *mockProvider) Scan(_ context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	m.roots = append([]string(nil), opts.Roots...)
	return m.worktrees, m.err
}

func TestScan_NoResults(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	var buf bytes.Buffer
	s := New([]adapter.DebrisProvider{
		&mockProvider{name: types.ToolCodex, err: errors.New("boom")},
		&mockProvider{
			name:      types.ToolClaude,
			worktrees: []types.DebrisInfo{{ID: "ok", Size: 50}},
		},
	})
	s.ErrorWriter = &buf

	result, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "scan:codex:boom") {
		t.Errorf("stderr = %q; want scan:codex:boom", buf.String())
	}
	if result.TotalCount != 1 {
		t.Errorf("TotalCount = %d; want 1", result.TotalCount)
	}
}

func TestScan_ProgressEvents(t *testing.T) {
	t.Parallel()
	var events []string
	var buf bytes.Buffer
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name:      types.ToolCodex,
			worktrees: []types.DebrisInfo{{ID: "ok", Tool: types.ToolCodex, Size: 100}},
		},
		&mockProvider{name: types.ToolClaude, err: errors.New("boom")},
	})
	s.ErrorWriter = &buf

	_, err := s.ScanWithOptions(context.Background(), types.ScanOptions{
		OnProgress: func(event types.ScanProgressEvent) {
			switch event.State {
			case types.ScanProgressStart:
				events = append(events, "start:"+string(event.Tool))
			case types.ScanProgressDone:
				events = append(events, "done:"+string(event.Tool)+":1:100")
				if event.Count != 1 {
					t.Errorf("done count = %d; want 1", event.Count)
				}
				if event.Size != 100 {
					t.Errorf("done size = %d; want 100", event.Size)
				}
			case types.ScanProgressError:
				events = append(events, "error:"+string(event.Tool)+":"+event.Err.Error())
			default:
				t.Errorf("unknown progress state %q", event.State)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"start:codex",
		"done:codex:1:100",
		"start:claude",
		"error:claude:boom",
	}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %v; want %v", events, want)
	}
}

func TestScan_ContextCancelOnEntry(t *testing.T) {
	t.Parallel()
	s := New([]adapter.DebrisProvider{
		&mockProvider{
			name:      types.ToolCodex,
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
	t.Parallel()
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
	t.Setenv("HOME", t.TempDir())

	result, err := Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestScanWithOptions_PassesNormalizedRootsToProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "workspace")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	provider := &mockProvider{name: types.ToolNodeModules}
	s := New([]adapter.DebrisProvider{provider})

	_, err := s.ScanWithOptions(context.Background(), types.ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(provider.roots, []string{want}) {
		t.Errorf("roots = %v; want %v", provider.roots, []string{want})
	}
}

func TestNormalizeRoots_DefaultHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	roots, err := NormalizeRoots(nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roots, []string{want}) {
		t.Errorf("roots = %v; want %v", roots, []string{want})
	}
}

func TestNormalizeRoots_TildeAndNestedDedup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}

	roots, err := NormalizeRoots([]string{"~/workspace", "~"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roots, []string{want}) {
		t.Errorf("roots = %v; want %v", roots, []string{want})
	}
}

func TestNormalizeRoots_RejectsOutsideHome(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	outside := filepath.Join(parent, "outside")
	os.MkdirAll(home, 0755)
	os.MkdirAll(outside, 0755)
	t.Setenv("HOME", home)

	_, err := NormalizeRoots([]string{outside})
	if err == nil {
		t.Fatal("expected outside home root to be rejected")
	}
	if !strings.Contains(err.Error(), "must be under") {
		t.Errorf("error = %v; want must be under", err)
	}
}

func TestNormalizeRoots_RejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	outside := filepath.Join(parent, "outside")
	os.MkdirAll(home, 0755)
	os.MkdirAll(outside, 0755)
	link := filepath.Join(home, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	t.Setenv("HOME", home)

	_, err := NormalizeRoots([]string{link})
	if err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if !strings.Contains(err.Error(), "must be under") {
		t.Errorf("error = %v; want must be under", err)
	}
}

func TestNew_NilProviders(t *testing.T) {
	t.Parallel()
	s := New(nil)
	if s.Providers != nil {
		t.Errorf("Providers = %v; want nil", s.Providers)
	}
}

func TestScanner_ErrorWriterDefault(t *testing.T) {
	t.Parallel()
	s := New([]adapter.DebrisProvider{})
	if s.errw() == nil {
		t.Error("errw() should default to os.Stderr, not nil")
	}
}

func TestScanner_ErrorWriterOverride(t *testing.T) {
	t.Parallel()
	s := New([]adapter.DebrisProvider{})
	buf := new(bytes.Buffer)
	s.ErrorWriter = buf
	var discardWriter io.Writer = buf
	_ = discardWriter
	if s.errw() != s.ErrorWriter {
		t.Error("errw() should return ErrorWriter when set")
	}
}
