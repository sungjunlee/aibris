package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// writeLP writes a length-prefixed field to h so concatenated fields are
// unambiguous.
func writeLP(h hash.Hash, b []byte) {
	var l [8]byte
	binary.BigEndian.PutUint64(l[:], uint64(len(b)))
	h.Write(l[:])
	h.Write(b)
}

// hashTree deterministically hashes every regular file under root (relative
// path, size, mtime seconds, content). Directories are not hashed directly. If
// skip reports true for a directory, that subtree is omitted; if it reports
// true for a file, that file is omitted. A missing root hashes to the empty
// tree. This underpins both the input fingerprint (skip the volatile cache) and
// the cache-identity check (skip nothing).
func hashTree(root string, skip func(path string) bool) (string, int, error) {
	type rec struct {
		rel  string
		size int64
		mt   int64
		sum  [32]byte
	}
	var recs []rec
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skip != nil && skip(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
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
		recs = append(recs, rec{
			rel:  filepath.ToSlash(rel),
			size: info.Size(),
			mt:   info.ModTime().Unix(),
			sum:  sha256.Sum256(data),
		})
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return emptyTreeHash(), 0, nil
		}
		return "", 0, err
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].rel < recs[j].rel })
	h := sha256.New()
	var l [8]byte
	for _, r := range recs {
		writeLP(h, []byte(r.rel))
		binary.BigEndian.PutUint64(l[:], uint64(r.size))
		h.Write(l[:])
		binary.BigEndian.PutUint64(l[:], uint64(r.mt))
		h.Write(l[:])
		h.Write(r.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), len(recs), nil
}

func emptyTreeHash() string {
	return hex.EncodeToString(sha256.New().Sum(nil))
}

// cacheRoots are the candidate aibris scan-cache locations under a home, across
// macOS (Library/Caches) and Linux ($HOME/.cache).
func cacheRoots(home string) []string {
	return []string{
		filepath.Join(home, "Library", "Caches"),
		filepath.Join(home, ".cache"),
	}
}

func underAny(path string, roots []string) bool {
	for _, r := range roots {
		if path == r || strings.HasPrefix(path, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// hashHomeInputs fingerprints the generated input stores under home, excluding
// the volatile scan cache. Because the synthetic home has no writer other than
// aibris's own cache, this fingerprint is stable across invocations and any
// change signals genuine drift.
func hashHomeInputs(home string) (string, int, error) {
	caches := cacheRoots(home)
	return hashTree(home, func(p string) bool { return underAny(p, caches) })
}

// hashCacheIdentity fingerprints the scan cache (empty string if no cache
// exists yet). It is captured per invocation as diagnostic evidence in the JSON
// report; it is NOT an acceptance gate. Determinism is gated by the inventory
// signature (InvSig/RetSig) and the home input fingerprint, which already catch
// any home mutation — the cache is an internal detail of `aibris scan`.
func hashCacheIdentity(home string) string {
	h := sha256.New()
	found := false
	for _, r := range cacheRoots(home) {
		if _, err := os.Stat(r); err != nil {
			continue
		}
		hexStr, _, err := hashTree(r, nil)
		if err != nil {
			continue
		}
		found = true
		writeLP(h, []byte(r))
		writeLP(h, []byte(hexStr))
	}
	if !found {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// scanObservation is one measured `scan` invocation.
type scanObservation struct {
	Wall        time.Duration
	Result      *scanResult
	CacheBefore string
	CacheAfter  string
	ExitCode    int
	Stderr      string
}

// controlledEnv returns the minimal, deterministic environment the protocol
// fixes for every measured invocation (mirrors the DOGFOOD `env -i` contract).
// It includes both Unix and Windows home/cache/temp conventions because the
// measured aibris binary must resolve all of them inside the fixture. The
// platform may choose a cache subdirectory below the supplied home.
func controlledEnv(home, tmpDir string) []string {
	homeDrive := filepath.VolumeName(home)
	homePath := strings.TrimPrefix(home, homeDrive)
	cacheHome := filepath.Join(home, ".cache")
	return []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"HOMEDRIVE=" + homeDrive,
		"HOMEPATH=" + homePath,
		"XDG_CACHE_HOME=" + cacheHome,
		"LOCALAPPDATA=" + cacheHome,
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"TMPDIR=" + tmpDir,
		"TEMP=" + tmpDir,
		"TMP=" + tmpDir,
		"LANG=C",
		"LC_ALL=C",
	}
}

// runScan runs `<bin> scan --root <home> --json` under the controlled
// environment, measuring wall-clock and capturing cache identity before/after.
func runScan(binPath, home, tmpDir string) (scanObservation, error) {
	obs := scanObservation{CacheBefore: hashCacheIdentity(home)}
	cmd := exec.Command(binPath, "scan", "--root", home, "--json")
	cmd.Env = controlledEnv(home, tmpDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	runErr := cmd.Run()
	obs.Wall = time.Since(start)
	obs.CacheAfter = hashCacheIdentity(home)
	obs.Stderr = stderr.String()
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			obs.ExitCode = ee.ExitCode()
		} else {
			return obs, fmt.Errorf("running scan: %w", runErr)
		}
	}
	res, err := parseScanOutput(stdout.Bytes())
	if err != nil {
		return obs, fmt.Errorf("parsing scan output (exit %d): %w\nstderr: %s", obs.ExitCode, err, obs.Stderr)
	}
	obs.Result = res
	return obs, nil
}
