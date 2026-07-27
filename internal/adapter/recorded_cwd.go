package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

type recordedCWDEvidence struct {
	cwds                    []string
	unverifiableFiles       []string
	unverifiableRecords     int
	firstUnverifiableRecord string
}

type recordedCWDEvidenceGatherer func(context.Context, string) (recordedCWDEvidence, error)

func classifyRecordedCWDEntry(
	ctx context.Context,
	entryPath string,
	gatherEvidence recordedCWDEvidenceGatherer,
) (types.EntryClass, string, string, error) {
	evidence, err := gatherEvidence(ctx, entryPath)
	if err != nil {
		return "", "", "", err
	}
	return classifyRecordedCWDEvidence(ctx, evidence)
}

func classifyRecordedCWDEvidence(
	ctx context.Context,
	evidence recordedCWDEvidence,
) (types.EntryClass, string, string, error) {
	var liveCWD string
	var absentCWD string
	var unverifiableCWD string
	var unavailableCWD string
	var unavailableAncestor string
	absentCount := 0
	for _, cwd := range evidence.cwds {
		if err := ctx.Err(); err != nil {
			return "", "", "", err
		}
		if _, err := os.Stat(cwd); err == nil {
			if liveCWD == "" {
				liveCWD = cwd
			}
		} else if errors.Is(err, os.ErrNotExist) {
			proven, ancestor, proofErr := recordedCWDAbsenceProven(cwd)
			switch {
			case proofErr != nil:
				if unverifiableCWD == "" {
					unverifiableCWD = cwd
				}
			case proven:
				absentCount++
				if absentCWD == "" {
					absentCWD = cwd
				}
			case unavailableCWD == "":
				unavailableCWD = cwd
				unavailableAncestor = ancestor
			}
		} else if unverifiableCWD == "" {
			unverifiableCWD = cwd
		}
	}

	if liveCWD != "" {
		return types.EntryClassLive,
			fmt.Sprintf("recorded cwd exists: %s (%d distinct recorded cwd(s) checked)", liveCWD, len(evidence.cwds)),
			projectNameFromRecordedCWD(liveCWD),
			nil
	}
	if unverifiableCWD != "" {
		return types.EntryClassUndetermined,
			"recorded cwd existence could not be verified: " + unverifiableCWD,
			projectNameFromRecordedCWD(unverifiableCWD),
			nil
	}
	if unavailableCWD != "" {
		return types.EntryClassUndetermined,
			fmt.Sprintf("recorded cwd surrounding tree is unavailable: %s (nearest existing ancestor: %s)",
				unavailableCWD, unavailableAncestor),
			projectNameFromRecordedCWD(unavailableCWD),
			nil
	}
	if evidence.unverifiableRecords > 0 {
		if absentCount > 0 {
			return types.EntryClassUndetermined,
				fmt.Sprintf("%d recorded cwd(s) do not exist, but %d session record(s) were unparseable or ended without a readable cwd; first: %s",
					absentCount, evidence.unverifiableRecords, evidence.firstUnverifiableRecord),
				projectNameFromRecordedCWD(absentCWD),
				nil
		}
		return types.EntryClassUndetermined,
			fmt.Sprintf("%d session record(s) were unparseable or ended without a readable cwd; first: %s",
				evidence.unverifiableRecords, evidence.firstUnverifiableRecord),
			"",
			nil
	}
	if len(evidence.unverifiableFiles) > 0 {
		if absentCount > 0 {
			return types.EntryClassUndetermined,
				fmt.Sprintf("%d recorded cwd(s) do not exist, but session metadata could not be verified: %s",
					absentCount, evidence.unverifiableFiles[0]),
				projectNameFromRecordedCWD(absentCWD),
				nil
		}
		return types.EntryClassUndetermined,
			"session metadata could not be verified: " + evidence.unverifiableFiles[0],
			"",
			nil
	}
	if len(evidence.cwds) == 0 {
		return types.EntryClassUndetermined, "no recorded cwd could be read from session metadata", "", nil
	}
	return types.EntryClassOrphaned,
		fmt.Sprintf("all %d distinct recorded cwd(s) do not exist; first: %s", len(evidence.cwds), absentCWD),
		projectNameFromRecordedCWD(absentCWD),
		nil
}

// recordedCWDAbsenceProven accepts ENOENT as deletion evidence only when the
// closest existing directory is in a container whose surrounding tree is
// expected to be locally available: the user's home or a temp root.
func recordedCWDAbsenceProven(cwd string) (bool, string, error) {
	for ancestor := cwd; ; ancestor = filepath.Dir(ancestor) {
		info, err := os.Lstat(ancestor)
		switch {
		case err == nil:
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(ancestor)
				if err != nil {
					// Deliberately fail closed: an unresolvable symlink makes absence unverifiable.
					return false, ancestor, nil //nolint:nilerr
				}
			}
			if !info.IsDir() {
				return false, ancestor, nil
			}
			plausible, err := plausibleRecordedCWDContainer(ancestor)
			return plausible, ancestor, err
		case !errors.Is(err, os.ErrNotExist):
			return false, ancestor, err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return false, ancestor, nil
		}
	}
}

func plausibleRecordedCWDContainer(path string) (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	roots := []string{home, os.TempDir()}
	if filepath.Separator == '/' {
		roots = append(roots, "/tmp", "/var/tmp")
	}
	for _, root := range roots {
		if pathWithinContainer(path, root) {
			return true, nil
		}
	}
	return false, nil
}

func pathWithinContainer(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(resolved)
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = filepath.Clean(resolved)
	}
	rel, err := filepath.Rel(root, path)
	return err == nil &&
		(rel == "." || (rel != ".." && !filepath.IsAbs(rel) &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}
