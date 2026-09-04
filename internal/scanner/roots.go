package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
)

func NormalizeRoots(rawRoots []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	resolvedHome, err := resolveExistingPath(home)
	if err != nil {
		return nil, fmt.Errorf("resolving home: %w", err)
	}

	if len(rawRoots) == 0 {
		rawRoots = []string{resolvedHome}
	}

	seen := make(map[string]bool)
	var roots []string
	for _, raw := range rawRoots {
		root, err := normalizeRoot(raw, resolvedHome)
		if err != nil {
			return nil, err
		}
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}

	sort.Strings(roots)
	var deduped []string
	for _, root := range roots {
		nested := false
		for _, parent := range deduped {
			if root == parent || adapter.IsWithin(parent, root) {
				nested = true
				break
			}
		}
		if !nested {
			deduped = append(deduped, root)
		}
	}
	return deduped, nil
}

func normalizeRoot(raw, home string) (string, error) {
	root := strings.TrimSpace(raw)
	if root == "" {
		return "", fmt.Errorf("scan root cannot be empty")
	}
	if root == "~" {
		root = home
	} else if strings.HasPrefix(root, "~/") {
		root = filepath.Join(home, strings.TrimPrefix(root, "~/"))
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("scan root %q must be absolute or start with ~", raw)
	}
	resolved, err := resolveExistingPath(root)
	if err != nil {
		return "", fmt.Errorf("resolving scan root %q: %w", raw, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("reading scan root %q: %w", raw, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("scan root %q is not a directory", raw)
	}
	if resolved != home && !adapter.IsWithin(home, resolved) && !isResolvedSystemTempDir(resolved) {
		return "", fmt.Errorf("scan root %q must be under %s", raw, home)
	}
	return resolved, nil
}

// isResolvedSystemTempDir reports whether resolved is the system temp dir
// after the same symlink cleanup applied to every other root. The resolved
// system temp dir is the only root permitted outside the home tree, and only
// when explicitly supplied as a root: default roots never include it.
func isResolvedSystemTempDir(resolved string) bool {
	tempDir, err := resolveExistingPath(os.TempDir())
	if err != nil {
		return false
	}
	return resolved == tempDir
}

func resolveExistingPath(path string) (string, error) {
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
