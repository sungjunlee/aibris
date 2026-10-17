package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleanjson"
)

func TestCLIContractProtectedLinkReferentPreservesOrphanOwner(t *testing.T) {
	for _, protected := range []string{"", ".ssh", "Documents"} {
		name := protected
		if name == "" {
			name = "unprotected control"
		}
		for _, referent := range []string{"owner", "descendant"} {
			t.Run(name+"/"+referent, func(t *testing.T) {
				home := t.TempDir()
				owner := filepath.Join(home, ".codex", "worktrees", "old-orphan")
				sentinel := filepath.Join(owner, "personal", "sentinel")
				writeCLIContractFixture(t, sentinel, "must survive protected cleanup")
				writeCLIContractFixture(t, filepath.Join(owner, ".git"),
					"gitdir: "+filepath.Join(home, "gone-repository", ".git", "worktrees", "old-orphan")+"\n")
				makeCLIContractTargetOld(t, owner)
				if protected != "" {
					target := owner
					if referent == "descendant" {
						target = filepath.Dir(sentinel)
					}
					if err := os.Symlink(target, filepath.Join(home, protected)); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				}

				result := runCLIContract(t, home, nil,
					"clean", "--no-guide", "--force", "--json", "--include-paths", "--age=1h", "--category=worktree")
				var receipt cleanjson.Receipt
				if err := json.Unmarshal([]byte(result.Stdout), &receipt); err != nil {
					t.Fatalf("invalid cleanup receipt: %v\nstdout:\n%s\nstderr:\n%s", err, result.Stdout, result.Stderr)
				}
				if receipt.DocumentType != "clean_receipt" || len(receipt.PhysicalTargets) != 1 {
					t.Fatalf("expected one orphan owner in a clean receipt: %+v", receipt)
				}
				target := receipt.PhysicalTargets[0]
				if protected == "" {
					if result.ExitCode != 0 || receipt.Totals.Removed != 1 || !target.PhysicalRemoved || target.FreedBytes <= 0 {
						t.Fatalf("unprotected control did not remove the owner: exit=%d receipt=%+v stderr=%s", result.ExitCode, receipt, result.Stderr)
					}
					if _, err := os.Lstat(owner); !os.IsNotExist(err) {
						t.Fatalf("unprotected control owner remains: %v", err)
					}
					return
				}
				if data, err := os.ReadFile(sentinel); err != nil || string(data) != "must survive protected cleanup" {
					t.Errorf("protected sentinel changed: %q, %v", data, err)
				}
				if _, err := os.Stat(owner); err != nil {
					t.Errorf("protected orphan owner removed: %v", err)
				}
				if receipt.Totals.Removed != 0 || receipt.Totals.FreedBytes != 0 || target.State == "removed" || target.PhysicalRemoved || target.FreedBytes != 0 {
					t.Errorf("receipt claims removal of protected owner: %+v", receipt)
				}
			})
		}
	}
}
