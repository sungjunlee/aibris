//go:build windows

package safedelete

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestWindowsProtectedJunctionRefusesReferentAndOwner(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, "orphan")
	target := filepath.Join(owner, "relocated")
	child := filepath.Join(target, "eligible-child")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "Documents")
	testutil.WindowsJunction(t, link, target)
	for _, path := range []string{link, target, owner} {
		if err := RemoveAll(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("RemoveAll(%s) = %v; want refusal", path, err)
		}
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("sentinel changed: %q, %v", data, err)
	}
	if err := RemoveAll(home, filepath.Join(link, "eligible-child")); err != nil {
		t.Fatalf("eligible junction child refused: %v", err)
	}
	if _, err := os.Stat(child); !os.IsNotExist(err) {
		t.Fatalf("eligible child remains: %v", err)
	}
}

func TestWindowsProtectedJunctionRecheckedAtRemoval(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, "orphan")
	target := filepath.Join(owner, "relocated")
	elsewhere := filepath.Join(home, "elsewhere")
	for _, path := range []string{target, elsewhere} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := filepath.Join(target, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "Documents")
	testutil.WindowsJunction(t, link, elsewhere)
	if err := Check(home, owner); err != nil {
		t.Fatalf("pre-retarget check: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	testutil.WindowsJunction(t, link, target)
	if err := RemoveAll(home, owner); !errors.Is(err, ErrRefused) {
		t.Errorf("retargeted junction allowed removal: %v", err)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("sentinel changed: %q, %v", data, err)
	}
}
