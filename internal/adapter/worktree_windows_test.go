//go:build windows

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

func TestWorktreeAdapter_WindowsMixedCaseRegisteredIdentity(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "MiXeDProfile")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	flipped := filepath.Join(parent, flipASCIICase(filepath.Base(home)))
	if strings.EqualFold(home, flipped) && home == flipped {
		t.Fatal("fixture case did not change")
	}
	testutil.SetHome(t, flipped)

	owner := filepath.Join(home, ".relay", "worktrees", "owner")
	createWorktreeGit(t, filepath.Join(owner, "leaf", "checkout"), filepath.Join(home, "parent"), "checkout")
	mixed := filepath.Join(home, ".relay", "worktrees", "mixed")
	createWorktreeGit(t, filepath.Join(mixed, "leaf", "checkout"), filepath.Join(home, "mixed-parent"), "checkout")
	if err := os.MkdirAll(filepath.Join(mixed, "bad", "nested", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	superpowers := filepath.Join(home, ".config", "superpowers", "worktrees", "sp-owner")
	createWorktreeGit(t, filepath.Join(superpowers, "leaf", "checkout"), filepath.Join(home, "sp-parent"), "checkout")

	scan := func(opts types.ScanOptions) []types.DebrisInfo {
		t.Helper()
		results, err := (&WorktreeAdapter{}).Scan(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		return results
	}

	full := scan(types.ScanOptions{})
	assertWindowsOwner(t, full, owner, windowsOwnerWant{
		id: "owner", project: "checkout", source: ".relay", status: types.WorktreeActive,
	})
	assertWindowsOwner(t, full, mixed, windowsOwnerWant{
		id: "mixed", project: "mixed", source: ".relay", status: types.WorktreePlain,
		reason: ".git marker is a directory",
	})
	assertWindowsOwner(t, full, superpowers, windowsOwnerWant{
		id: "sp-owner", project: "checkout", source: "superpowers", status: types.WorktreeActive,
	})

	explicit := scan(types.ScanOptions{Roots: []string{owner}})
	if len(explicit) != 1 {
		t.Fatalf("explicit root rows = %+v; want the one relay owner", explicit)
	}
	assertWindowsOwner(t, explicit, owner, windowsOwnerWant{
		id: "owner", project: "checkout", source: ".relay", status: types.WorktreeActive,
	})
}

func TestWorktreeAdapter_WindowsBlockedAliasIsNotReintroduced(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "MiXeDProfile")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.SetHome(t, filepath.Join(parent, flipASCIICase(filepath.Base(home))))

	actual := filepath.Join(home, "RealTarget", "worktrees")
	createWorktreeGit(t, filepath.Join(actual, "alias-target"), filepath.Join(home, "parent"), "alias-target")
	registered := filepath.Join(home, ".relay", "worktrees")
	if err := os.MkdirAll(filepath.Dir(registered), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actual, registered); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "OutSide")
	outsideContainer := filepath.Join(outside, "worktrees")
	createWorktreeGit(t, filepath.Join(outsideContainer, "escaped"), filepath.Join(outside, "parent"), "escaped")
	gstack := filepath.Join(home, ".gstack", "worktrees")
	if err := os.MkdirAll(filepath.Dir(gstack), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideContainer, gstack); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range results {
		if row.ID == "alias-target" || row.ID == "escaped" ||
			canonicalExistingPath(row.Path) == canonicalExistingPath(actual) ||
			canonicalExistingPath(row.Path) == canonicalExistingPath(filepath.Join(actual, "alias-target")) ||
			canonicalExistingPath(row.Path) == canonicalExistingPath(outsideContainer) ||
			strings.Contains(strings.ToLower(row.Path), strings.ToLower("OutSide")) {
			t.Fatalf("blocked alias target reappeared: %+v", row)
		}
	}
}

type windowsOwnerWant struct {
	id      string
	project string
	source  string
	status  types.WorktreeStatus
	reason  string
}

func assertWindowsOwner(t *testing.T, rows []types.DebrisInfo, owner string, want windowsOwnerWant) {
	t.Helper()
	wantPath, err := filepath.EvalSymlinks(owner)
	if err != nil {
		t.Fatal(err)
	}
	wantPath = filepath.Clean(wantPath)
	var matched []types.DebrisInfo
	for _, row := range rows {
		if canonicalExistingPath(row.Path) == canonicalExistingPath(owner) {
			matched = append(matched, row)
		}
	}
	if len(matched) != 1 {
		t.Fatalf("%s rows = %d; want one physical owner: %+v", want.id, len(matched), rows)
	}
	got := matched[0]
	if got.Path != wantPath {
		t.Fatalf("%s Path = %q; want resolved spelling %q", want.id, got.Path, wantPath)
	}
	if !strings.Contains(got.Path, "MiXeDProfile") {
		t.Fatalf("%s Path = %q; want filesystem spelling MiXeDProfile", want.id, got.Path)
	}
	if got.ID != want.id || got.Project != want.project || got.Source != want.source || got.Status != want.status {
		t.Fatalf("%s row = %+v; want id=%s project=%s source=%s status=%s",
			want.id, got, want.id, want.project, want.source, want.status)
	}
	if want.reason != "" && !strings.Contains(got.Reason, want.reason) {
		t.Fatalf("%s reason = %q; want %q", want.id, got.Reason, want.reason)
	}
}

func flipASCIICase(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
