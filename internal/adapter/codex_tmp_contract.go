package adapter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var errCodexTmpUnknownSymlink = errors.New(codexTmpReasonUnknownSymlink)

type codexTmpWriterClass string

const (
	codexTmpWriterGUI        codexTmpWriterClass = "codex-gui"
	codexTmpWriterCLI        codexTmpWriterClass = "codex-cli"
	codexTmpWriterApplyPatch codexTmpWriterClass = "apply-patch"
	codexTmpWriterSupervisor codexTmpWriterClass = "supervisor"
)

type codexTmpMemberKind string

const (
	codexTmpMemberDir     codexTmpMemberKind = "dir"
	codexTmpMemberFile    codexTmpMemberKind = "file"
	codexTmpMemberSymlink codexTmpMemberKind = "symlink"
)

type codexTmpMember struct {
	RelPath    string
	Kind       codexTmpMemberKind
	Identity   string
	LinkTarget string
	Size       int64
	ModTime    time.Time
}

type codexTmpSnapshot struct {
	CanonicalPath string
	FenceToken    string
	Members       []codexTmpMember
}

func (s *codexTmpSnapshot) equal(other *codexTmpSnapshot) bool {
	if s == nil || other == nil {
		return false
	}
	if s.CanonicalPath != other.CanonicalPath || s.FenceToken != other.FenceToken {
		return false
	}
	if len(s.Members) != len(other.Members) {
		return false
	}
	for i := range s.Members {
		if s.Members[i] != other.Members[i] {
			return false
		}
	}
	return true
}

type codexTmpLayout struct {
	Version        string
	Permitted      map[string]codexTmpMemberKind
	SymlinkTargets map[string]string
}

func (l codexTmpLayout) account(members []codexTmpMember) string {
	seen := make(map[string]bool, len(members))
	for _, member := range members {
		want, ok := l.Permitted[member.RelPath]
		if !ok {
			return codexTmpReasonUnknownLayout
		}
		if want != member.Kind {
			return codexTmpReasonUnknownLayout
		}
		if member.Kind == codexTmpMemberSymlink {
			expected, known := l.SymlinkTargets[member.RelPath]
			if !known || expected != member.LinkTarget {
				return codexTmpReasonUnknownSymlink
			}
		}
		seen[member.RelPath] = true
	}
	for rel := range l.Permitted {
		if !seen[rel] {
			return codexTmpReasonUnknownLayout
		}
	}
	return ""
}

type codexTmpFence struct {
	unit  string
	token string
	owner *codexTmpCooperativeExclusion
}

func (f *codexTmpFence) Token() string {
	if f == nil {
		return ""
	}
	return f.token
}

func (f *codexTmpFence) Held() bool {
	if f == nil || f.owner == nil {
		return false
	}
	return f.owner.held(f.unit, f.token)
}

func (f *codexTmpFence) Release() {
	if f == nil || f.owner == nil {
		return
	}
	f.owner.release(f.unit, f.token)
}

type codexTmpCooperativeExclusion struct {
	writers      map[codexTmpWriterClass]bool
	mu           sync.Mutex
	tokens       map[string]string
	seq          int
	afterAcquire func(unit string)
}

func newCodexTmpCooperativeExclusion(writers ...codexTmpWriterClass) *codexTmpCooperativeExclusion {
	participating := make(map[codexTmpWriterClass]bool, len(writers))
	for _, writer := range writers {
		participating[writer] = true
	}
	return &codexTmpCooperativeExclusion{
		writers: participating,
		tokens:  make(map[string]string),
	}
}

func (e *codexTmpCooperativeExclusion) complete() bool {
	if e == nil {
		return false
	}
	for _, writer := range codexTmpRequiredWriters {
		if !e.writers[writer] {
			return false
		}
	}
	return true
}

func (e *codexTmpCooperativeExclusion) Acquire(unit string) (*codexTmpFence, error) {
	if e == nil || !e.complete() {
		return nil, fmt.Errorf("%s", codexTmpReasonNoExclusion)
	}
	e.mu.Lock()
	if _, held := e.tokens[unit]; held {
		e.mu.Unlock()
		return nil, fmt.Errorf("%s", codexTmpReasonNoExclusion)
	}
	e.seq++
	token := strconv.Itoa(e.seq)
	e.tokens[unit] = token
	e.mu.Unlock()
	fence := &codexTmpFence{unit: unit, token: token, owner: e}
	if e.afterAcquire != nil {
		e.afterAcquire(unit)
	}
	return fence, nil
}

func (e *codexTmpCooperativeExclusion) held(unit, token string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tokens[unit] == token
}

func (e *codexTmpCooperativeExclusion) release(unit, token string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tokens[unit] == token {
		delete(e.tokens, unit)
	}
}

func (e *codexTmpCooperativeExclusion) lose(unit string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.tokens, unit)
}

func inventoryCodexTmpUnit(ctx context.Context, unit string) ([]codexTmpMember, error) {
	var members []codexTmpMember
	err := filepath.WalkDir(unit, func(path string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(unit, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			members = append(members, makeCodexTmpMember(rel, info, ""))
			return nil
		}
		linkTarget := ""
		kind := memberKind(info)
		if kind == codexTmpMemberSymlink {
			linkTarget, err = os.Readlink(path)
			if err != nil {
				return err
			}
			if !symlinkTargetInsideUnit(unit, path, linkTarget) {
				return errCodexTmpUnknownSymlink
			}
		}
		members = append(members, makeCodexTmpMember(rel, info, linkTarget))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(members, func(i, j int) bool {
		return members[i].RelPath < members[j].RelPath
	})
	return members, nil
}

func makeCodexTmpMember(rel string, info os.FileInfo, linkTarget string) codexTmpMember {
	return codexTmpMember{
		RelPath:    rel,
		Kind:       memberKind(info),
		Identity:   memberIdentity(info, linkTarget),
		LinkTarget: linkTarget,
		Size:       info.Size(),
		ModTime:    info.ModTime(),
	}
}

func memberKind(info os.FileInfo) codexTmpMemberKind {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return codexTmpMemberSymlink
	case mode.IsDir():
		return codexTmpMemberDir
	default:
		return codexTmpMemberFile
	}
}

func memberIdentity(info os.FileInfo, linkTarget string) string {
	return strings.Join([]string{
		info.Mode().String(),
		strconv.FormatInt(info.Size(), 10),
		strconv.FormatInt(info.ModTime().UnixNano(), 10),
		linkTarget,
		memberSysIdentity(info),
	}, "\x00")
}

func symlinkTargetInsideUnit(unit, linkPath, target string) bool {
	resolved := target
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(linkPath), target)
	}
	return IsWithin(unit, filepath.Clean(resolved))
}

// deleteCodexTmpUnit removes one admitted direct child while exclusion is
// held. It revalidates the scan-time snapshot immediately before mutation,
// unlinks contained symlinks without following them, and aborts the whole
// unit on lock loss or mismatch. The tmp root is never deleted.
func (a *CodexTmpAdapter) deleteCodexTmpUnit(ctx context.Context, unit string, expected *codexTmpSnapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tmpRoot, err := a.tmpRootForUnit(unit)
	if err != nil {
		return err
	}
	if filepath.Clean(unit) == tmpRoot {
		return fmt.Errorf("%s", codexTmpReasonTmpRoot)
	}
	fence, reason := a.acquireExclusion(unit)
	if fence == nil {
		return fmt.Errorf("%s", reason)
	}
	defer fence.Release()
	if !fence.Held() {
		return fmt.Errorf("%s", codexTmpReasonFenceLost)
	}

	members, err := inventoryCodexTmpUnit(ctx, unit)
	if err != nil {
		return err
	}
	if !fence.Held() {
		return fmt.Errorf("%s", codexTmpReasonFenceLost)
	}
	fresh := &codexTmpSnapshot{
		CanonicalPath: filepath.Clean(unit),
		FenceToken:    fence.Token(),
		Members:       members,
	}
	if expected != nil {
		compare := *expected
		compare.FenceToken = fence.Token()
		compare.CanonicalPath = filepath.Clean(compare.CanonicalPath)
		if !fresh.equal(&compare) {
			return fmt.Errorf("%s", codexTmpReasonSnapshotMismatch)
		}
	}
	if reason := a.ownershipReason(members); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	recheckMembers, err := inventoryCodexTmpUnit(ctx, unit)
	if err != nil {
		return err
	}
	recheck := &codexTmpSnapshot{
		CanonicalPath: filepath.Clean(unit),
		FenceToken:    fence.Token(),
		Members:       recheckMembers,
	}
	if !fresh.equal(recheck) {
		return fmt.Errorf("%s", codexTmpReasonSnapshotMismatch)
	}
	if !fence.Held() {
		return fmt.Errorf("%s", codexTmpReasonFenceLost)
	}
	return removeCodexTmpUnit(unit)
}

func removeCodexTmpUnit(unit string) error {
	var paths []string
	err := filepath.WalkDir(unit, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.Count(paths[i], string(filepath.Separator)) > strings.Count(paths[j], string(filepath.Separator))
	})
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() {
			if err := os.Remove(path); err != nil {
				return err
			}
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}
