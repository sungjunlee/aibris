package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlledEnvIsolatesHomeCacheAndTemp(t *testing.T) {
	home := t.TempDir()
	tmpDir := t.TempDir()
	env := controlledEnv(home, tmpDir)

	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("controlled environment entry %q has no separator", entry)
		}
		if _, exists := values[key]; exists {
			t.Fatalf("controlled environment repeats %q: %v", key, env)
		}
		values[key] = value
	}

	homeDrive := filepath.VolumeName(home)
	want := map[string]string{
		"HOME":           home,
		"USERPROFILE":    home,
		"HOMEDRIVE":      homeDrive,
		"HOMEPATH":       strings.TrimPrefix(home, homeDrive),
		"XDG_CACHE_HOME": filepath.Join(home, ".cache"),
		"LOCALAPPDATA":   filepath.Join(home, ".cache"),
		"TMPDIR":         tmpDir,
		"TEMP":           tmpDir,
		"TMP":            tmpDir,
	}
	for key, wantValue := range want {
		if got := values[key]; got != wantValue {
			t.Errorf("controlledEnv %s = %q; want %q", key, got, wantValue)
		}
	}

	for key, value := range values {
		t.Setenv(key, value)
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(home, cacheDir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		t.Fatalf("os.UserCacheDir() = %q; want a path inside fixture home %q", cacheDir, home)
	}
	if got := os.TempDir(); got != tmpDir {
		t.Fatalf("os.TempDir() = %q; want isolated temp %q", got, tmpDir)
	}
}

func writeFile(t *testing.T, path, content string, mt time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func TestHashTreeIsDeterministicAndContentSensitive(t *testing.T) {
	root := t.TempDir()
	mt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFile(t, filepath.Join(root, "a.txt"), "alpha", mt)
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "beta", mt)

	h1, n1, err := hashTree(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 2 {
		t.Fatalf("files = %d; want 2", n1)
	}
	h2, _, err := hashTree(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hashTree not deterministic: %s vs %s", h1, h2)
	}

	// Changing content changes the hash.
	writeFile(t, filepath.Join(root, "a.txt"), "changed", mt)
	h3, _, err := hashTree(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h3 {
		t.Fatalf("hashTree did not detect content change")
	}
}

func TestHashTreeSkipOmitsSubtree(t *testing.T) {
	root := t.TempDir()
	mt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFile(t, filepath.Join(root, "keep.txt"), "keep", mt)
	writeFile(t, filepath.Join(root, "Library", "Caches", "aibris", "cache.json"), "{}", mt)

	skip := func(p string) bool { return underAny(p, cacheRoots(root)) }
	before, nBefore, err := hashTree(root, skip)
	if err != nil {
		t.Fatal(err)
	}
	if nBefore != 1 {
		t.Fatalf("files (cache skipped) = %d; want 1", nBefore)
	}

	// Mutating only the cache must not change the input fingerprint.
	writeFile(t, filepath.Join(root, "Library", "Caches", "aibris", "cache.json"), `{"changed":true}`, mt)
	after, _, err := hashTree(root, skip)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("input fingerprint changed when only the cache changed")
	}
}

func TestHashHomeInputsExcludesCache(t *testing.T) {
	home := t.TempDir()
	mt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	writeFile(t, filepath.Join(home, ".codex", "sessions", "2024", "01", "01", "rollout-x.jsonl"), "{}\n", mt)

	fp1, n1, err := hashHomeInputs(home)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 1 {
		t.Fatalf("input files = %d; want 1", n1)
	}

	// Writing a scan cache under the home must not move the input fingerprint.
	writeFile(t, filepath.Join(home, "Library", "Caches", "aibris", "codex-activity.json"), `{"created_at":"x"}`, mt)
	fp2, _, err := hashHomeInputs(home)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("input fingerprint changed after cache write")
	}

	// Cache identity, by contrast, must now be non-empty.
	if id := hashCacheIdentity(home); id == "" {
		t.Fatalf("cache identity is empty though a cache exists")
	}
}

func TestHashTreeMissingRootIsEmpty(t *testing.T) {
	h, n, err := hashTree(filepath.Join(t.TempDir(), "does-not-exist"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || h != emptyTreeHash() {
		t.Fatalf("missing root = (%s,%d); want empty tree", h, n)
	}
}
