package retention

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCodexSessionsOrcaAndExtraHomesAggregateOnceReadOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	extra := filepath.Join(t.TempDir(), ".codex")
	alias := filepath.Join(home, "alias")
	if err := os.Symlink(extra, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("AIBRIS_CODEX_HOMES", extra+string(filepath.ListSeparator)+orca+string(filepath.ListSeparator)+alias)
	var leaves []string
	for _, codexHome := range []string{filepath.Join(home, ".codex"), extra, orca} {
		dir := filepath.Join(codexHome, "sessions", "2026", "10", "08")
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		leaf := filepath.Join(dir, "rollout-session.jsonl")
		if err := os.WriteFile(leaf, []byte(validMetadata(home)+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		setModTime(t, leaf, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
		leaves = append(leaves, leaf)
	}
	for _, scenario := range []struct {
		name string
		opts types.ScanOptions
		want int
	}{
		{"default", types.ScanOptions{Roots: []string{home}}, 3},
		{"explicit-home", types.ScanOptions{Roots: []string{home}, ExplicitRoots: true}, 2},
		{"explicit-orca", types.ScanOptions{Roots: []string{orca}, ExplicitRoots: true}, 1},
		{"excluded", types.ScanOptions{Roots: []string{filepath.Join(home, "unrelated")}, ExplicitRoots: true}, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			projection, err := NewCodexSessionsProvider().Scan(context.Background(), scenario.opts)
			if err != nil || projection.Partial {
				t.Fatalf("projection = %+v, %v; want complete", projection, err)
			}
			if scenario.want == 0 {
				if len(projection.Buckets) != 0 {
					t.Fatalf("excluded projection = %+v", projection)
				}
			} else if bucket := onlyBucket(t, projection); bucket.UnitCount != scenario.want || bucket.MemberCount != scenario.want {
				t.Fatalf("bucket = %+v; want %d distinct physical sessions", bucket, scenario.want)
			}
		})
	}
	for _, leaf := range leaves {
		content, err := os.ReadFile(leaf)
		if err != nil || string(content) != validMetadata(home)+"\n" {
			t.Fatalf("inventory modified a session: %q, %v", content, err)
		}
	}
}
