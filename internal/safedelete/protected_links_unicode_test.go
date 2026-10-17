package safedelete

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

var protectedUnicodeSpellings = []struct {
	name      string
	candidate string
	protected string
}{
	{name: "NFC-to-NFD", candidate: "caf\u00e9s", protected: "cafe\u0301s"},
	{name: "NFD-to-NFC", candidate: "cafe\u0301s", protected: "caf\u00e9s"},
}

func TestProtectedLinksUnicodeReferentAndOwner(t *testing.T) {
	for _, spelling := range protectedUnicodeSpellings {
		for _, scope := range []string{"leaf", "owner"} {
			for _, timing := range []string{"before-check", "retarget-after-check"} {
				t.Run(spelling.name+"/"+scope+"/"+timing, func(t *testing.T) {
					home, candidateOwner, protectedOwner, aliases := protectedUnicodePair(t, spelling.candidate, spelling.protected)
					candidate := candidateOwner
					if scope == "leaf" {
						candidate = filepath.Join(candidateOwner, "notes")
					}
					protectedTarget := filepath.Join(protectedOwner, "notes")
					link := filepath.Join(home, "Documents")
					if timing == "retarget-after-check" {
						initial := filepath.Join(home, "initial-documents")
						if err := os.Mkdir(initial, 0o755); err != nil {
							t.Fatal(err)
						}
						protectedUnicodeSymlink(t, initial, link)
						if err := Check(home, candidate); err != nil {
							t.Fatalf("pre-retarget candidate refused: %v", err)
						}
						if err := os.Remove(link); err != nil {
							t.Fatal(err)
						}
					}
					protectedUnicodeSymlink(t, protectedTarget, link)

					if timing == "before-check" {
						// Children remain eligible even when the two Unicode spellings
						// identify the same protected directory.
						child := filepath.Join(candidateOwner, "notes", "ordinary-child")
						for _, path := range []string{child, filepath.Join(link, "ordinary-child")} {
							if err := Check(home, path); err != nil {
								t.Errorf("ordinary child refused at %q: %v", path, err)
							}
						}
						if err := RemoveAll(home, child); err != nil {
							t.Fatalf("ordinary child removal refused: %v", err)
						}
						if _, err := os.Lstat(child); !os.IsNotExist(err) {
							t.Fatalf("ordinary child remains: %v", err)
						}
						assertProtectedUnicodeSentinel(t, filepath.Join(protectedTarget, "sentinel"))

						checkProtectedUnicodeDecision(t, home, candidate, aliases)
					}
					// No post-retarget Check on candidate: RemoveAll must obtain
					// fresh protection evidence at its own mutation boundary.
					checkProtectedUnicodeRemoval(t, home, candidate, protectedOwner, aliases)
				})
			}
		}
	}
}

func TestProtectedLinksUnicodeMissingAndDanglingTail(t *testing.T) {
	for _, spelling := range protectedUnicodeSpellings {
		for _, tail := range []string{"missing", "dangling-link"} {
			t.Run(spelling.name+"/"+tail, func(t *testing.T) {
				home, candidateOwner, protectedOwner, aliases := protectedUnicodePair(t, spelling.candidate, spelling.protected)
				target := filepath.Join(protectedOwner, "missing", "notes")
				if tail == "dangling-link" {
					forward := filepath.Join(protectedOwner, "forward")
					protectedUnicodeSymlink(t, "missing", forward)
					target = filepath.Join(forward, "notes")
				}
				protectedUnicodeSymlink(t, target, filepath.Join(home, "Documents"))
				for _, path := range []string{
					candidateOwner,
					filepath.Join(candidateOwner, "missing"),
					filepath.Join(candidateOwner, "missing", "notes"),
				} {
					checkProtectedUnicodeDecision(t, home, path, aliases)
				}
				for _, path := range []string{
					filepath.Join(candidateOwner, "missing-unrelated"),
					filepath.Join(candidateOwner, "missing", "notes", "ordinary-child"),
				} {
					if err := Check(home, path); err != nil {
						t.Errorf("missing protected suffix overprotected %q: %v", path, err)
					}
				}
				checkProtectedUnicodeDecision(t, home, protectedOwner, true)
				checkProtectedUnicodeRemoval(t, home, candidateOwner, protectedOwner, aliases)
			})
		}
	}
}

// Both spellings are created before comparing identity. On an aliasing
// filesystem they name one directory; on a distinct-name filesystem they
// are two independent directories, and the unprotected one must stay eligible.
func protectedUnicodePair(t *testing.T, candidateName, protectedName string) (home, candidate, protected string, aliases bool) {
	t.Helper()
	home = t.TempDir()
	testutil.SetHome(t, home)
	candidate = filepath.Join(home, candidateName)
	protected = filepath.Join(home, protectedName)
	for _, owner := range []string{candidate, protected} {
		child := filepath.Join(owner, "notes", "ordinary-child")
		if err := os.MkdirAll(child, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{filepath.Join(owner, "notes", "sentinel"), filepath.Join(child, "payload")} {
			if err := os.WriteFile(path, []byte("synthetic Unicode sentinel"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	candidateInfo, err := os.Stat(candidate)
	if err != nil {
		t.Fatal(err)
	}
	protectedInfo, err := os.Stat(protected)
	if err != nil {
		t.Fatal(err)
	}
	aliases = os.SameFile(candidateInfo, protectedInfo)
	t.Logf("NFC/NFD directory identities alias: %t", aliases)
	return home, candidate, protected, aliases
}

func protectedUnicodeSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}

func checkProtectedUnicodeDecision(t *testing.T, home, candidate string, protected bool) {
	t.Helper()
	err := Check(home, candidate)
	if protected && !errors.Is(err, ErrRefused) {
		t.Errorf("Check(%q) = %v; want protected identity refusal", candidate, err)
	} else if !protected && err != nil {
		t.Errorf("Check(%q) = %v; distinct Unicode spelling must remain eligible", candidate, err)
	}
}

func checkProtectedUnicodeRemoval(t *testing.T, home, candidate, protectedOwner string, aliases bool) {
	t.Helper()
	err := RemoveAll(home, candidate)
	if aliases {
		if !errors.Is(err, ErrRefused) {
			t.Errorf("RemoveAll(%q) = %v; want protected identity refusal", candidate, err)
		}
		if _, err := os.Stat(candidate); err != nil {
			t.Errorf("protected candidate changed: %v", err)
		}
	} else {
		if err != nil {
			t.Errorf("independent Unicode spelling removal refused: %v", err)
		}
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Errorf("independent Unicode spelling was not removed: %v", err)
		}
	}
	assertProtectedUnicodeSentinel(t, filepath.Join(protectedOwner, "notes", "sentinel"))
}

func assertProtectedUnicodeSentinel(t *testing.T, path string) {
	t.Helper()
	if data, err := os.ReadFile(path); err != nil || string(data) != "synthetic Unicode sentinel" {
		t.Errorf("protected Unicode sentinel changed at %q: %q, %v", path, data, err)
	}
}
