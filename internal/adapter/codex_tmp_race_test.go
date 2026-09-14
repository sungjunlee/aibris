package adapter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCodexTmpAdapter_RacesLeaveUnitIntactAndIneligible(t *testing.T) {
	writerClasses := append([]codexTmpWriterClass(nil), codexTmpRequiredWriters...)
	cases := []struct {
		name string
		race func(t *testing.T, unit string, a *CodexTmpAdapter)
		want string
	}{
		{
			name: "creation",
			race: func(t *testing.T, unit string, _ *CodexTmpAdapter) {
				mustWrite(t, filepath.Join(unit, "raced"), []byte("new"))
			},
			want: codexTmpReasonSnapshotMismatch,
		},
		{
			name: "mutation",
			race: func(t *testing.T, unit string, _ *CodexTmpAdapter) {
				mustWrite(t, filepath.Join(unit, "known"), []byte("changed-bytes"))
			},
			want: codexTmpReasonSnapshotMismatch,
		},
		{
			name: "rename",
			race: func(t *testing.T, unit string, _ *CodexTmpAdapter) {
				if err := os.Rename(unit, unit+"-moved"); err != nil {
					t.Fatal(err)
				}
			},
			want: "",
		},
		{
			name: "exclusion-loss",
			race: func(_ *testing.T, unit string, a *CodexTmpAdapter) {
				a.exclusion.afterAcquire = func(path string) {
					if path == unit {
						a.exclusion.lose(path)
					}
				}
			},
			want: codexTmpReasonFenceLost,
		},
	}

	for _, writer := range writerClasses {
		for _, tc := range cases {
			t.Run(string(writer)+"/"+tc.name, func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				tmpRoot := filepath.Join(home, ".codex", "tmp")
				unit := filepath.Join(tmpRoot, "unit")
				if err := os.MkdirAll(unit, 0755); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(unit, "known"), []byte("ok"))

				a := admittedTestAdapter(t, tmpRoot, "unit")
				verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if len(verdicts) != 1 || !verdicts[0].Eligible {
					t.Fatalf("pre-race verdict = %+v; want admitted", verdicts)
				}

				tc.race(t, unit, a)
				err = a.deleteCodexTmpUnit(context.Background(), unit, verdicts[0].snapshot)
				if err == nil {
					t.Fatal("delete succeeded after race; want abort")
				}
				if tc.want != "" && !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("delete err = %v; want %q", err, tc.want)
				}

				check := unit
				if tc.name == "rename" {
					check = unit + "-moved"
				}
				unitIntact(t, check)
				if tc.name == "creation" {
					results, scanErr := a.Scan(context.Background(), types.ScanOptions{})
					if scanErr != nil {
						t.Fatal(scanErr)
					}
					if len(results) != 0 {
						t.Fatalf("raced unit became eligible: %+v", results)
					}
				}
			})
		}
	}
}

func TestCodexTmpAdapter_AllOrNothingMismatchRemovesNothing(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	unit := filepath.Join(tmpRoot, "unit")
	if err := os.MkdirAll(filepath.Join(unit, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(unit, "sub", "a.txt"), []byte("a"))
	mustWrite(t, filepath.Join(unit, "sub", "b.txt"), []byte("b"))

	a := admittedTestAdapter(t, tmpRoot, "unit")
	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(unit, "sub", "a.txt"), []byte("mutated"))
	err = a.deleteCodexTmpUnit(context.Background(), unit, verdicts[0].snapshot)
	if err == nil || !strings.Contains(err.Error(), codexTmpReasonSnapshotMismatch) {
		t.Fatalf("delete err = %v; want snapshot mismatch", err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if !fileExists(t, filepath.Join(unit, "sub", name)) {
			t.Fatalf("partial deletion removed %s", name)
		}
	}
}

func TestCodexTmpAdapter_TmpRootSymlinkIsNotFollowed(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	realTmp := filepath.Join(home, "real-tmp")
	if err := os.MkdirAll(filepath.Join(realTmp, "unit"), 0755); err != nil {
		t.Fatal(err)
	}
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTmp, filepath.Join(codexHome, "tmp")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	a := &CodexTmpAdapter{}
	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != 1 || verdicts[0].Eligible || verdicts[0].Reason != codexTmpReasonSymlinkRoot {
		t.Fatalf("verdicts = %+v; want symlink tmp root", verdicts)
	}
	if !fileExists(t, filepath.Join(realTmp, "unit")) {
		t.Fatal("symlinked tmp contents were mutated")
	}
}
