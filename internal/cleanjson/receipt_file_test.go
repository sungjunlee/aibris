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
