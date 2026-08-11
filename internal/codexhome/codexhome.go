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

// Homes returns the primary Codex home followed by any configured extra
// homes, deduplicated.
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
	return homes, nil
}
