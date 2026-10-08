package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// The parent rename changes only external Git evidence. The cached owner's
// inode and mtime remain identical, so path identity cannot detect restoration.
func TestCachedOrphanedWorktreeForceRevalidatesGitEvidence(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "human", true: "json"}[jsonOutput], func(t *testing.T) {
			home, repository, owner := newExecutorWorktree(t, "cached-orphan")
			testutil.SetHome(t, home)
			sentinel := filepath.Join(owner, "uncommitted-sentinel")
			writeGitFixtureFile(t, owner, "uncommitted-sentinel", "keep my changes")
			parked := repository + "-parked"
			if err := os.Rename(repository, parked); err != nil {
				t.Fatal(err)
			}
			cacheOrphanedOwner(t, home, owner)
			before, err := os.Stat(owner)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(parked, repository); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(owner)
			if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
				t.Fatalf("external Git restoration changed owner identity: %v", err)
			}
			args := []string{"clean", "--no-guide", "--force", "--age=1h", "--category=worktree"}
			if jsonOutput {
				args = append(args, "--json")
			}
			stdout, stderr, runErr := runCleanJSONProcess(t, binary, home, args...)
			if runErr == nil {
				t.Fatalf("cached orphaned cleanup succeeded after parent restoration: stdout=%s stderr=%s", stdout, stderr)
			}
			assertPathExists(t, sentinel)
			if jsonOutput {
				var receipt cleanJSONReceipt
				if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
					t.Fatalf("invalid JSON receipt: %v\n%s", err, stdout)
				}
				if receipt.Plan.Evidence.Source != string(scanSourceCached) || receipt.Status != "failed" || receipt.Totals.Failed != 1 || receipt.Totals.FreedBytes != 0 || !strings.Contains(stdout, "worktree_evidence_changed") {
					t.Fatalf("missing cached Git drift refusal in JSON: %s", stdout)
				}
			} else if !strings.Contains(stdout+stderr, "active") || !strings.Contains(stdout, "cached") {
				t.Fatalf("missing cached Git drift reason: stdout=%s stderr=%s", stdout, stderr)
			}
		})
	}
}

func TestOrphanedWorktreeRevalidatesDuringConfirmation(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, drift := range []string{"parent-restored", "marker-replaced", "member-added", "invalid-marker", "mixed-members", "unreadable-marker"} {
		t.Run(drift, func(t *testing.T) {
			home, repository, owner, first, _ := newExecutorMultiMemberUnit(t)
			testutil.SetHome(t, home)
			parked := repository + "-parked"
			if err := os.Rename(repository, parked); err != nil {
				t.Fatal(err)
			}
			cacheOrphanedOwner(t, home, owner)
			marker := filepath.Join(first, ".git")
			markerContents, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			mutate := func() {
				ownerInfo, statErr := os.Stat(owner)
				if statErr != nil {
					t.Fatal(statErr)
				}
				switch drift {
				case "parent-restored":
					err = os.Rename(parked, repository)
				case "marker-replaced":
					// Same bytes, a different marker inode, unchanged outer owner.
					err = os.Rename(marker, filepath.Join(home, "old-marker"))
					if err == nil {
						err = os.WriteFile(marker, markerContents, 0o644)
					}
				case "member-added":
					added := filepath.Join(owner, "c-added")
					err = os.Mkdir(added, 0o755)
					if err == nil {
						err = os.WriteFile(filepath.Join(added, ".git"), []byte("gitdir: "+filepath.Join(home, "missing-gitdir")+"\n"), 0o644)
					}
				case "invalid-marker":
					err = os.WriteFile(marker, []byte("invalid Git metadata\n"), 0o644)
				case "mixed-members":
					err = os.Mkdir(filepath.Join(owner, "invalid-member"), 0o755)
					if err == nil {
						err = os.WriteFile(filepath.Join(owner, "invalid-member", "payload"), []byte("keep"), 0o644)
					}
				case "unreadable-marker":
					err = os.Rename(marker, filepath.Join(home, "old-marker"))
					if err == nil {
						// A symlink loop fails deterministically even under root.
						err = os.Symlink(".git", marker)
						if err != nil {
							t.Skipf("symlink unavailable: %v", err)
						}
					}
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(owner, ownerInfo.ModTime(), ownerInfo.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			stdout, stderr, runErr := runOrphanCleanAtConfirmation(t, binary, home, mutate)
			if runErr == nil {
				t.Fatalf("cleanup succeeded despite %s during confirmation: stdout=%s stderr=%s", drift, stdout, stderr)
			}
			assertPathExists(t, owner)
			assertPathExists(t, filepath.Join(first, "README.md"))
			wantReason := map[string]string{
				"parent-restored":   "classified active",
				"marker-replaced":   "marker changed",
				"member-added":      "member set changed",
				"invalid-marker":    "malformed",
				"mixed-members":     "missing .git marker",
				"unreadable-marker": "reading worktree marker",
			}[drift]
			if !strings.Contains(stdout+stderr, "evidence") || !strings.Contains(stdout+stderr, wantReason) {
				t.Fatalf("missing Git evidence refusal: stdout=%s stderr=%s", stdout, stderr)
			}
		})
	}
}

func TestCachedOrphanedWorktreeJSONRefusesInvalidEvidence(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, drift := range []string{"invalid", "mixed", "unreadable"} {
		t.Run(drift, func(t *testing.T) {
			home, repository, owner, first, _ := newExecutorMultiMemberUnit(t)
			testutil.SetHome(t, home)
			if err := os.Rename(repository, repository+"-parked"); err != nil {
				t.Fatal(err)
			}
			cacheOrphanedOwner(t, home, owner)
			marker := filepath.Join(first, ".git")
			switch drift {
			case "invalid":
				if err := os.WriteFile(marker, []byte("invalid marker\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "mixed":
				writeGitFixtureFile(t, first, "nested/payload", "keep")
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if err := os.Rename(marker, filepath.Join(home, "old-marker")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(".git", marker); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			stdout, stderr, err := runCleanJSONProcess(t, binary, home,
				"clean", "--json", "--force", "--no-guide", "--age=1h", "--category=worktree")
			if err == nil || strings.Contains(stderr, home) {
				t.Fatalf("invalid evidence cleanup: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			var receipt cleanJSONReceipt
			if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Plan.Evidence.Source != string(scanSourceCached) || receipt.Status != "failed" || receipt.Totals.Failed != 1 || receipt.Totals.FreedBytes != 0 || !strings.Contains(stdout, "worktree_evidence_changed") {
				t.Fatalf("missing JSON evidence refusal: %s", stdout)
			}
			assertPathExists(t, filepath.Join(first, "README.md"))
		})
	}
}

func TestOrphanedWorktreeUnchangedConfirmationDeletes(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home, repository, owner := newExecutorWorktree(t, "unchanged-orphan")
	testutil.SetHome(t, home)
	if err := os.Rename(repository, repository+"-parked"); err != nil {
		t.Fatal(err)
	}
	cacheOrphanedOwner(t, home, owner)
	stdout, stderr, err := runOrphanCleanAtConfirmation(t, binary, home, func() {})
	if err != nil || !pathDoesNotExist(owner) || !strings.Contains(stdout, "removed:") {
		t.Fatalf("unchanged orphan cleanup failed: %v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

func cacheOrphanedOwner(t *testing.T, home, owner string) {
	t.Helper()
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(owner, old, old); err != nil {
		t.Fatal(err)
	}
	items, err := adapter.NewWorktreeAdapter().Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil || len(items) == 0 {
		t.Fatalf("scanning orphan fixture: %v items=%+v", err, items)
	}
	for _, item := range items {
		if item.Path != owner || item.Status != types.WorktreeOrphaned {
			t.Fatalf("expected orphaned owner %q: %+v", owner, item)
		}
	}
	saveCleanCacheFixture(t, home, items)
}

type confirmationMutationWriter struct {
	writer io.Writer
	mutate func()
}

func (w confirmationMutationWriter) Write(p []byte) (int, error) {
	w.mutate()
	return w.writer.Write(p)
}

// Wait for the real prompt before changing evidence and supplying approval.
// Pipes are the deterministic seam; no sleeps or production test hook needed.
func runOrphanCleanAtConfirmation(t *testing.T, binary, home string, mutate func()) (string, string, error) {
	t.Helper()
	command := exec.Command(binary, "clean", "--no-guide", "--age=1h", "--category=worktree")
	command.Env = cliContractEnv(os.Environ(), home)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = stdin.Close() })
	watchdog := time.AfterFunc(time.Minute, func() { _ = command.Process.Kill() })
	defer watchdog.Stop()
	transcript, answerErr := answerGuidedCleanPrompts(stdout, confirmationMutationWriter{stdin, mutate}, []guidedCleanPromptAnswer{{prompt: "Proceed? [y/N]: ", reply: "y\n"}})
	_ = stdin.Close()
	rest, _ := io.ReadAll(stdout)
	transcript += string(rest)
	runErr := command.Wait()
	if answerErr != nil {
		t.Fatalf("confirmation seam failed: %v stdout=%s stderr=%s", answerErr, transcript, stderr.String())
	}
	return transcript, stderr.String(), runErr
}
