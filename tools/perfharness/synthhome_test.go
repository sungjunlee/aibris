package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

// fileRec is a normalized record of one regular file for tree comparison.
type fileRec struct {
	Rel   string
	Size  int64
	MTime int64
	Hash  string
}

// snapshotTree walks root and returns a sorted, root-relative record of every
// regular file (path, size, mtime seconds, content sha256). Directory mtimes
// are intentionally excluded: the retention provider keys buckets off leaf file
// mtimes, which Generate pins deterministically via Chtimes.
func snapshotTree(t *testing.T, root string) []fileRec {
	t.Helper()
	var recs []fileRec
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		recs = append(recs, fileRec{
			Rel:   filepath.ToSlash(rel),
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
			Hash:  hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Rel < recs[j].Rel })
	return recs
}

// TestGenerateIsDeterministic verifies that generating into the same path twice
// yields a byte-identical tree (files, sizes, mtimes, content). This is the
// property the four-pair protocol relies on for zero drift by construction.
func TestGenerateIsDeterministic(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	spec := DefaultHomeSpec(HomeOpts{
		Months:           []string{"2024-01", "2024-02"},
		FilesPerMonth:    5,
		MinBytes:         1024,
		MaxBytes:         8192,
		LiveEvery:        2,
		NodeModulesFiles: 3,
	})

	if err := Generate(home, spec); err != nil {
		t.Fatalf("first generate: %v", err)
	}
	first := snapshotTree(t, home)
	if len(first) == 0 {
		t.Fatalf("generated tree is empty")
	}

	if err := os.RemoveAll(home); err != nil {
		t.Fatalf("removing home: %v", err)
	}
	if err := Generate(home, spec); err != nil {
		t.Fatalf("second generate: %v", err)
	}
	second := snapshotTree(t, home)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("generated tree is not deterministic:\nfirst=%d recs\nsecond=%d recs", len(first), len(second))
	}
}

// TestGenerateRolloutFirstRecordIsValid verifies the first JSONL record of a
// generated rollout parses as a session_meta the Codex-sessions provider
// accepts (producer codex_cli_rs, supported version, absolute cwd).
func TestGenerateRolloutFirstRecordIsValid(t *testing.T) {
	home := t.TempDir()
	spec := HomeSpec{
		Rollouts: []RolloutSpec{{
			Year:    2024,
			Month:   3,
			Day:     5,
			ModTime: time.Date(2024, 3, 5, 12, 0, 0, 0, time.UTC),
			Size:    2048,
			CWDTgt:  "workspace/absent-2024-03-0",
		}},
	}
	if err := Generate(home, spec); err != nil {
		t.Fatalf("generate: %v", err)
	}

	path := filepath.Join(home, ".codex", "sessions", "2024", "03", "05", "rollout-000000.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading rollout: %v", err)
	}
	idx := bytes.IndexByte(data, '\n')
	if idx < 0 {
		t.Fatalf("rollout has no newline-terminated first record")
	}
	var rec struct {
		Type    string `json:"type"`
		Payload struct {
			ID         string `json:"id"`
			CWD        string `json:"cwd"`
			Originator string `json:"originator"`
			CLIVersion string `json:"cli_version"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data[:idx], &rec); err != nil {
		t.Fatalf("first record is not valid JSON: %v\nline=%s", err, data[:idx])
	}
	if rec.Type != "session_meta" {
		t.Errorf("type = %q; want session_meta", rec.Type)
	}
	if rec.Payload.Originator != "codex_cli_rs" {
		t.Errorf("originator = %q; want codex_cli_rs", rec.Payload.Originator)
	}
	if rec.Payload.CLIVersion != "1.2.3" {
		t.Errorf("cli_version = %q; want 1.2.3", rec.Payload.CLIVersion)
	}
	if !filepath.IsAbs(rec.Payload.CWD) {
		t.Errorf("cwd = %q; want absolute", rec.Payload.CWD)
	}
	wantCWD := filepath.Join(home, "workspace/absent-2024-03-0")
	if rec.Payload.CWD != wantCWD {
		t.Errorf("cwd = %q; want %q", rec.Payload.CWD, wantCWD)
	}
}

// TestGenerateExactApparentSizes verifies each rollout leaf has exactly the
// requested apparent byte count (the provider reports handleInfo.Size()).
func TestGenerateExactApparentSizes(t *testing.T) {
	home := t.TempDir()
	spec := HomeSpec{
		Rollouts: []RolloutSpec{
			{Year: 2024, Month: 1, Day: 1, ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Size: 4096, CWDTgt: "workspace/absent-a"},
			{Year: 2024, Month: 2, Day: 2, ModTime: time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC), Size: 8192, CWDTgt: "workspace/absent-b"},
		},
	}
	if err := Generate(home, spec); err != nil {
		t.Fatalf("generate: %v", err)
	}
	// Locate the two rollout leaves and compare their apparent sizes.
	var sizes []int64
	err := filepath.WalkDir(filepath.Join(home, ".codex", "sessions"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sizes = append(sizes, info.Size())
		return nil
	})
	if err != nil {
		t.Fatalf("walking sessions: %v", err)
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] < sizes[j] })
	want := []int64{4096, 8192}
	if !reflect.DeepEqual(sizes, want) {
		t.Fatalf("rollout sizes = %v; want %v", sizes, want)
	}
}

func itoa4(v int) string {
	b := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0; i-- {
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b)
}

func itoa2(v int) string {
	return string([]byte{byte('0' + v/10), byte('0' + v%10)})
}

// TestDefaultHomeSpecShape verifies the builder produces the expected month
// buckets, live/orphan split, and auxiliary node_modules store.
func TestDefaultHomeSpecShape(t *testing.T) {
	months := []string{"2024-01", "2024-02", "2024-03"}
	spec := DefaultHomeSpec(HomeOpts{
		Months:           months,
		FilesPerMonth:    6,
		MinBytes:         512,
		MaxBytes:         4096,
		LiveEvery:        3, // 1 live per 3 -> 2 live per month of 6
		NodeModulesFiles: 2,
	})

	if len(spec.Rollouts) != len(months)*6 {
		t.Fatalf("rollouts = %d; want %d", len(spec.Rollouts), len(months)*6)
	}
	// LiveTargets: per month, indices 0 and 3 are live -> proj-0 and proj-1.
	if len(spec.LiveTargets) != 2 {
		t.Fatalf("live targets = %v; want 2 (proj-0, proj-1)", spec.LiveTargets)
	}
	// Every live target referenced by a rollout must be created.
	liveSet := make(map[string]bool, len(spec.LiveTargets))
	for _, lt := range spec.LiveTargets {
		liveSet[lt] = true
	}
	liveCount := 0
	for _, r := range spec.Rollouts {
		if liveSet[r.CWDTgt] {
			liveCount++
		}
	}
	if liveCount != len(months)*2 {
		t.Fatalf("live rollouts = %d; want %d", liveCount, len(months)*2)
	}
	if len(spec.NodeModules) != 1 || len(spec.NodeModules[0].Files) != 2 {
		t.Fatalf("node modules = %+v; want one dir with 2 files", spec.NodeModules)
	}

	// Distinct month buckets present.
	seen := make(map[string]bool)
	for _, r := range spec.Rollouts {
		seen[itoa4(r.Year)+"-"+itoa2(r.Month)] = true
	}
	if len(seen) != len(months) {
		t.Fatalf("distinct buckets = %v; want %d", seen, len(months))
	}
}

// TestGenerateCreatesLiveTargets verifies live cwd directories are created and
// orphan targets are left absent (so they classify orphaned under the home).
func TestGenerateCreatesLiveTargets(t *testing.T) {
	home := t.TempDir()
	spec := HomeSpec{
		Rollouts: []RolloutSpec{
			{Year: 2024, Month: 1, Day: 1, ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Size: 1024, CWDTgt: "workspace/proj-0"},
			{Year: 2024, Month: 1, Day: 2, ModTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Size: 1024, CWDTgt: "workspace/absent-1"},
		},
		LiveTargets: []string{"workspace/proj-0"},
	}
	if err := Generate(home, spec); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(home, "workspace/proj-0")); err != nil || !fi.IsDir() {
		t.Fatalf("live target not created as dir: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "workspace/absent-1")); !os.IsNotExist(err) {
		t.Fatalf("orphan target should be absent, got err=%v", err)
	}
}
