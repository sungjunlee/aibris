package scanreport

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestScanReclaimUsesApparentBytesAcrossProviders(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	// A successful allocated-size du response must not change scan accounting.
	bin := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.WriteFile(filepath.Join(bin, "du"), []byte("#!/bin/sh\nshift\nfor p do printf '0\\t%s\\n' \"$p\"; done\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("GOCACHE", "off")
	const size = int64(1 << 30)
	old := time.Now().Add(-30 * 24 * time.Hour)
	paths := []string{filepath.Join(home, "project", "node_modules"), filepath.Join(home, ".gradle", "caches")}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(path, "sparse")
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{file, path} {
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := scanner.New([]adapter.DebrisProvider{&adapter.NodeModulesAdapter{}, &adapter.BuildCacheAdapter{}})
	result, err := s.ScanWithOptions(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worktrees) != 2 {
		t.Fatalf("items = %v; want dependency and cache", result.Worktrees)
	}
	for _, item := range result.Worktrees {
		if item.Size != size {
			t.Errorf("%s size = %d; want apparent %d", item.Path, item.Size, size)
		}
	}
	if result.TotalSize != 2*size || result.PhysicalTotalBytes != 2*size {
		t.Errorf("scan totals = %d, %d; want %d", result.TotalSize, result.PhysicalTotalBytes, 2*size)
	}
	policy := testPolicy()
	view := FromResult(result, policy)
	if view.DefaultCleanSize != 2*size || SizeByLabel(view.ReclaimPaths, labelDefaultDelete) != 2*size {
		t.Errorf("reclaim = %d, %v; want apparent %d", view.DefaultCleanSize, view.ReclaimPaths, 2*size)
	}
	out := EncodeJSON(FromResultJSON(result))
	if out.Summary.TotalSize != 2*size || out.Summary.PhysicalTotalBytes != 2*size {
		t.Errorf("JSON totals = %+v; want %d", out.Summary, 2*size)
	}
	// Physical dedup counts owners, not shared blocks or evidence rows.
	result.Worktrees = append(result.Worktrees, result.Worktrees[0])
	result.TotalSize += size
	out = EncodeJSON(FromResultJSON(result))
	if out.Summary.TotalSize != 3*size || out.Summary.PhysicalTotalBytes != 2*size {
		t.Errorf("duplicate owner totals = %+v; want row sum %d, owner sum %d", out.Summary, 3*size, 2*size)
	}
}
