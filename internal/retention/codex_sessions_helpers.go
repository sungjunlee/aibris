package retention

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/codexsession"
)

// Path, stat, and parse helpers for the Codex sessions inventory.
// Session listing and walk orchestration stay in codex_sessions.go.

// retentionUnknownBucket collects units whose effective timestamp is unusable.
// It is a visible aggregate like any other bucket; it is never cleanable.
const retentionUnknownBucket = "unknown"

// supportedCodexVersion gates orphan classification on a recognizable Codex
// CLI producer version so unknown-format files never count as orphans.
var supportedCodexVersion = regexp.MustCompile(
	`^(0|1)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?(\+[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`,
)

// codexSessionsRoot returns the exact bounded store root: the sessions
// directory of the resolved Codex home ($CODEX_HOME, or ~/.codex when unset).
func codexSessionsRoot() (string, error) {
	codexHome, err := codexhome.Home()
	if err != nil {
		return "", err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(codexHome); resolveErr == nil {
		codexHome = resolved
	}
	return filepath.Join(codexHome, "sessions"), nil
}

// classifiableMetadata reports whether the first-record metadata is from a
// recognized Codex CLI producer with a usable absolute recorded cwd.
func classifiableMetadata(metadata codexsession.Metadata) bool {
	return metadata.Producer == "codex_cli_rs" &&
		supportedCodexVersion.MatchString(metadata.Version) &&
		usableRecordedCWD(metadata.CWD)
}

// rootsCoveringCodexHome returns the root selection extended with the
// resolved Codex home when it is not already covered, so a CODEX_HOME
// outside the scan roots is inventoried rather than silently deselected.
func rootsCoveringCodexHome(roots []string) []string {
	codexHome, err := codexhome.Home()
	if err != nil || len(roots) == 0 || storeSelected(codexHome, roots) {
		return roots
	}
	return append(append([]string(nil), roots...), codexHome)
}

func storeSelected(store string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	store = filepath.Clean(store)
	for _, root := range roots {
		root = filepath.Clean(root)
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = filepath.Clean(resolved)
		}
		relative, err := filepath.Rel(root, store)
		if err == nil && relative != ".." && !filepath.IsAbs(relative) &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func validYear(value string) bool {
	if len(value) != 4 {
		return false
	}
	year, err := strconv.Atoi(value)
	return err == nil && year >= 1
}

func validMonth(value string) bool {
	if len(value) != 2 {
		return false
	}
	month, err := strconv.Atoi(value)
	return err == nil && month >= 1 && month <= 12
}

func validDay(yearValue, monthValue, dayValue string) bool {
	if len(dayValue) != 2 {
		return false
	}
	year, yearErr := strconv.Atoi(yearValue)
	month, monthErr := strconv.Atoi(monthValue)
	day, dayErr := strconv.Atoi(dayValue)
	if yearErr != nil || monthErr != nil || dayErr != nil || day < 1 || day > 31 {
		return false
	}
	date := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return date.Year() == year && int(date.Month()) == month && date.Day() == day
}

func isRolloutName(name string) bool {
	return strings.HasPrefix(name, "rollout-") &&
		strings.HasSuffix(name, ".jsonl") &&
		len(name) > len("rollout-.jsonl")
}

func bucketFromModTime(modTime time.Time) string {
	if modTime.IsZero() {
		return retentionUnknownBucket
	}
	utc := modTime.UTC()
	if utc.Year() < 1 || utc.Year() > 9999 {
		return retentionUnknownBucket
	}
	return fmt.Sprintf("%04d-%02d", utc.Year(), utc.Month())
}

func usableRecordedCWD(cwd string) bool {
	return cwd != "" && !strings.ContainsRune(cwd, '\x00') && filepath.IsAbs(cwd)
}
