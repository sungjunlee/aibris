package cleanjson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestWriteOwnerOnlyJSONForcesOwnerOnlyMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "receipt.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := WriteOwnerOnlyJSON(path, map[string]string{"status": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("receipt file mode = %v; want exactly 0600", perm)
	}
}

func TestWriteOwnerOnlyJSONEncodesIndentedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "receipt.json")
	document := map[string]any{"status": "succeeded", "count": 1}
	if err := WriteOwnerOnlyJSON(path, document); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("receipt file is not JSON: %v\n%s", err, raw)
	}
	if got["status"] != "succeeded" {
		t.Fatalf("status = %v; want succeeded", got["status"])
	}
	if !strings.Contains(string(raw), "\n  ") {
		t.Fatalf("receipt file is not indented:\n%s", raw)
	}
}

func TestRejectReceiptSinkOverlap(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "node_modules")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	targets := []types.DebrisInfo{{Path: target}}

	inside := filepath.Join(target, "receipt.json")
	if err := RejectReceiptSinkOverlap(inside, targets); err == nil {
		t.Fatal("expected overlap refusal for a sink inside a cleanup target")
	} else if !strings.Contains(err.Error(), "is inside a cleanup target") {
		t.Fatalf("overlap error = %v; want inside a cleanup target", err)
	}

	if err := RejectReceiptSinkOverlap(target, targets); err == nil {
		t.Fatal("expected overlap refusal for a sink that is the cleanup target")
	}

	outside := filepath.Join(root, "receipt.json")
	if err := RejectReceiptSinkOverlap(outside, targets); err != nil {
		t.Fatalf("outside sink rejected: %v", err)
	}
}

func TestResolveReceiptSinkResolvesParentSymlink(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := os.Mkdir(realParent, 0755); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(root, "link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	got, err := ResolveReceiptSink(filepath.Join(linkParent, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(realParent)
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(want, "receipt.json")
	if got != want {
		t.Fatalf("resolved sink = %q; want %q", got, want)
	}
}

func TestRejectReceiptSinkOverlapRefusesLeafAlias(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "command-audit.log")
	const payload = "original-audit-bytes\n"
	if err := os.WriteFile(target, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "receipt.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err := RejectReceiptSinkOverlap(link, []types.DebrisInfo{{Path: target}})
	if err == nil || !strings.Contains(err.Error(), "symlink or reparse point") {
		t.Fatalf("alias error = %v; want a symlink or reparse refusal", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != payload {
		t.Fatalf("target after refusal = %q, %v; want original bytes", got, readErr)
	}
}

func TestRejectReceiptSinkOverlapFollowsParentSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "node_modules")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(root, "link")
	if err := os.Symlink(target, linkParent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	err := RejectReceiptSinkOverlap(filepath.Join(linkParent, "receipt.json"), []types.DebrisInfo{{Path: target}})
	if err == nil || !strings.Contains(err.Error(), "is inside a cleanup target") {
		t.Fatalf("parent symlink overlap = %v; want the sink inside the target", err)
	}
}

func TestReceiptSinkWriteDoesNotFollowSwappedLeaf(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "command-audit.log")
	const payload = "original-audit-bytes\n"
	if err := os.WriteFile(victim, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(root, "receipt.json")
	sink, err := PrepareReceiptSink(receiptPath, []types.DebrisInfo{{Path: victim}})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if err := os.Symlink(victim, receiptPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	err = sink.Write(map[string]string{"status": "succeeded"})
	if err == nil {
		t.Fatal("write followed a leaf alias created after the check")
	}
	got, readErr := os.ReadFile(victim)
	if readErr != nil || string(got) != payload {
		t.Fatalf("victim after swapped alias = %q, %v; want original bytes", got, readErr)
	}
	info, statErr := os.Lstat(receiptPath)
	if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("receipt path = mode %v, err %v; want the alias left unfollowed", info.Mode(), statErr)
	}
}

func TestReceiptSinkWriteStaysInBoundParentAfterRetarget(t *testing.T) {
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	otherParent := filepath.Join(root, "other")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(otherParent, 0o755); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(root, "link")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	sink, err := PrepareReceiptSink(filepath.Join(linkParent, "receipt.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	if err := os.Remove(linkParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherParent, linkParent); err != nil {
		t.Fatal(err)
	}

	if err := sink.Write(map[string]string{"status": "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(realParent, "receipt.json")); err != nil {
		t.Fatalf("receipt missing from the directory checked before the retarget: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(otherParent, "receipt.json")); !os.IsNotExist(err) {
		t.Fatalf("receipt followed the retargeted parent symlink: %v", err)
	}
}
