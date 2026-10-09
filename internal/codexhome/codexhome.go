// Package codexhome resolves the Codex home directory.
//
// The Codex CLI itself honors CODEX_HOME: hosts that run Codex under a
// sandboxed or otherwise overridden home keep their entire store there
// instead of ~/.codex. Every aibris surface that reads Codex state must
// resolve the home through this package so that store is not invisible.
package codexhome

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// extraHomesEnv lists additional Codex homes in a PATH-style list of
// absolute paths. Entries are opt-in extra stores reported alongside the
// primary home.
const extraHomesEnv = "AIBRIS_CODEX_HOMES"

// Home returns the primary Codex home directory: $CODEX_HOME when set and
// non-empty, otherwise ~/.codex.
func Home() (string, error) {
	if env := strings.TrimSpace(os.Getenv("CODEX_HOME")); env != "" {
		return filepath.Clean(env), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// UnconfiguredDefaultHome returns ~/.codex only when CODEX_HOME is empty.
// Explicit settings cannot prove absence: a missing configured path may be a typo.
func UnconfiguredDefaultHome() (string, error) {
	if os.Getenv("CODEX_HOME") != "" {
		return "", nil
	}
	return Home()
}

// ExtraHomes returns the additional Codex homes listed in $AIBRIS_CODEX_HOMES,
// a PATH-style separator-delimited list of absolute paths. Empty and relative
// entries are ignored; the list order is preserved.
func ExtraHomes() []string {
	var homes []string
	for _, entry := range filepath.SplitList(os.Getenv(extraHomesEnv)) {
		entry = strings.TrimSpace(entry)
		if entry == "" || !filepath.IsAbs(entry) {
			continue
		}
		homes = append(homes, filepath.Clean(entry))
	}
	return homes
}

// OrcaHome returns Orca's verified macOS default Codex home, or an empty
// string when absent or unrecognized. Only layout metadata is inspected;
// config contents and auth.json are never read.
func OrcaHome() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, "Library", "Application Support", "orca", "codex-runtime-home", "home")
	config, err := os.Lstat(filepath.Join(path, "config.toml"))
	if err != nil || !config.Mode().IsRegular() {
		return ""
	}
	sessions, err := os.Lstat(filepath.Join(path, "sessions"))
	if err != nil || !sessions.IsDir() || sessions.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return path
}

// Homes returns the primary Codex home followed by configured extras and
// Orca's verified macOS home, deduplicated in that order.
func Homes() ([]string, error) {
	primary, err := Home()
	if err != nil {
		return nil, err
	}
	homes := []string{primary}
	seen := map[string]bool{primary: true}
	for _, extra := range ExtraHomes() {
		if seen[extra] {
			continue
		}
		seen[extra] = true
		homes = append(homes, extra)
	}
	if orca := OrcaHome(); orca != "" {
		orcaInfo, err := os.Stat(orca)
		if err != nil {
			return homes, nil
		}
		for _, home := range homes {
			// Configured aliases keep their spelling and position. Discovery
			// must not add a second copy of the same physical home.
			if info, err := os.Stat(home); err == nil && os.SameFile(info, orcaInfo) {
				return homes, nil
			}
		}
		homes = append(homes, orca)
	}
	return homes, nil
}
