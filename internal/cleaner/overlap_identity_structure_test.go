package cleaner

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cleaner after the same-package extract.
var (
	_ = canonicalExistingPathIdentity
	_ = (canonicalPathIdentity).unchanged
	_ = (canonicalPathIdentity).matches
	_ = canonicalOverlapRelation
	_ = ambiguousAgentStateMayOverlap
	_ = resolvePathWithUnresolvedSuffix
	_ = BuildOverlapSafetyPlan
	_ = (OverlapSafetyPlan).AllowedTargets
	_ = (OverlapSafetyPlan).ComponentForTarget
	_ = PathContains
)

func TestOverlapIdentityHelpersLiveApartFromPlanEntry(t *testing.T) {
	helperNames := []string{
		"canonicalExistingPathIdentity",
		"unchanged",
		"matches",
		"canonicalOverlapRelation",
		"ambiguousAgentStateMayOverlap",
		"resolvePathWithUnresolvedSuffix",
	}
	facadeNames := []string{
		"BuildOverlapSafetyPlan",
		"AllowedTargets",
		"ComponentForTarget",
		"PathContains",
	}

	wanted := make(map[string]string, len(helperNames)+len(facadeNames))
	for _, name := range helperNames {
		wanted[name] = "overlap_identity.go"
	}
	for _, name := range facadeNames {
		wanted[name] = "overlap.go"
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parse cleaner: %v", err)
	}

	owners := make(map[string][]string)
	for _, pkg := range pkgs {
		for filename, file := range pkg.Files {
			base := filepath.Base(filename)
			if strings.HasSuffix(base, "_test.go") {
				continue
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if _, ok := wanted[fn.Name.Name]; !ok {
					continue
				}
				owners[fn.Name.Name] = append(owners[fn.Name.Name], base)
			}
		}
	}

	for name, owner := range wanted {
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestOverlapIdentityHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cleaner callers still resolve to the helper implementations.
	helpers := []any{
		canonicalExistingPathIdentity,
		(canonicalPathIdentity).unchanged,
		(canonicalPathIdentity).matches,
		canonicalOverlapRelation,
		ambiguousAgentStateMayOverlap,
		resolvePathWithUnresolvedSuffix,
	}
	public := []any{
		BuildOverlapSafetyPlan,
		(OverlapSafetyPlan).AllowedTargets,
		(OverlapSafetyPlan).ComponentForTarget,
		PathContains,
	}
	for i, fn := range helpers {
		if fn == nil {
			t.Errorf("helper %d is nil", i)
		}
	}
	for i, fn := range public {
		if fn == nil {
			t.Errorf("public %d is nil", i)
		}
	}

	var (
		_ func(string) (canonicalPathIdentity, error)                                                                              = canonicalExistingPathIdentity
		_ func(canonicalPathIdentity) error                                                                                        = (canonicalPathIdentity).unchanged
		_ func(canonicalPathIdentity, canonicalPathIdentity) error                                                                 = (canonicalPathIdentity).matches
		_ func(string, string) (OverlapSafetyRelation, bool)                                                                       = canonicalOverlapRelation
		_ func(canonicalPathIdentity, string) (bool, error)                                                                        = ambiguousAgentStateMayOverlap
		_ func(string, int) (string, error)                                                                                        = resolvePathWithUnresolvedSuffix
		_ func(context.Context, OverlapSafetyEvidence, []types.DebrisInfo, AgentStateRevalidatorLookup) (OverlapSafetyPlan, error) = BuildOverlapSafetyPlan
		_ func(OverlapSafetyPlan) []types.DebrisInfo                                                                               = (OverlapSafetyPlan).AllowedTargets
		_ func(OverlapSafetyPlan, types.DebrisInfo) (OverlapSafetyComponent, bool)                                                 = (OverlapSafetyPlan).ComponentForTarget
		_ func(string, string) bool                                                                                                = PathContains
	)

	planSource := readCleanerSource(t, "overlap.go")
	helperSource := readCleanerSource(t, "overlap_identity.go")
	if !strings.Contains(planSource, "func BuildOverlapSafetyPlan(") {
		t.Error("BuildOverlapSafetyPlan is not defined in overlap.go")
	}
	if !strings.Contains(planSource, "func PathContains(") {
		t.Error("PathContains is not defined in overlap.go")
	}
	for _, name := range []string{
		"canonicalExistingPathIdentity",
		"canonicalOverlapRelation",
		"ambiguousAgentStateMayOverlap",
		"resolvePathWithUnresolvedSuffix",
	} {
		if strings.Contains(planSource, "func "+name+"(") {
			t.Errorf("%s is still defined in overlap.go", name)
		}
		if !strings.Contains(helperSource, "func "+name+"(") {
			t.Errorf("%s is not defined in overlap_identity.go", name)
		}
	}
	for _, name := range []string{
		"unchanged",
		"matches",
	} {
		if strings.Contains(planSource, "func (identity canonicalPathIdentity) "+name+"(") {
			t.Errorf("%s is still defined in overlap.go", name)
		}
		if !strings.Contains(helperSource, "func (identity canonicalPathIdentity) "+name+"(") {
			t.Errorf("%s is not defined in overlap_identity.go", name)
		}
	}
}
