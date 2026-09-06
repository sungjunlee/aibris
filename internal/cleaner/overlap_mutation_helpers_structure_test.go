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
	_ = overlapValidationForObligations
	_ = (*OverlapSafetyValidation).passObligation
	_ = (*OverlapSafetyValidation).blockObligation
	_ = (*OverlapSafetyValidation).blockOutcomeAtPath
	_ = (*OverlapSafetyValidation).ensureBlockedOutcome
	_ = revalidationOutcomeKey
	_ = overlapRefusalBlockingPath
	_ = overlapMatchClassification
	_ = overlapMatchForPath
	_ = mergedAgentStateObligations
	_ = (OverlapSafetyComponent{}).ValidateBeforeMutation
	_ = (OverlapSafetyComponent{}).ValidateBeforeMutationWithReport
)

func TestOverlapMutationHelpersLiveApartFromMutationEntry(t *testing.T) {
	helperNames := []string{
		"overlapValidationForObligations",
		"passObligation",
		"blockObligation",
		"blockOutcomeAtPath",
		"ensureBlockedOutcome",
		"revalidationOutcomeKey",
		"overlapRefusalBlockingPath",
		"overlapMatchClassification",
		"overlapMatchForPath",
		"mergedAgentStateObligations",
	}
	entryNames := []string{
		"ValidateBeforeMutation",
		"ValidateBeforeMutationWithReport",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "overlap_mutation_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "overlap_mutation.go"
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

func TestOverlapMutationHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cleaner callers still resolve to the helper implementations.
	helpers := []any{
		overlapValidationForObligations,
		(*OverlapSafetyValidation).passObligation,
		(*OverlapSafetyValidation).blockObligation,
		(*OverlapSafetyValidation).blockOutcomeAtPath,
		(*OverlapSafetyValidation).ensureBlockedOutcome,
		revalidationOutcomeKey,
		overlapRefusalBlockingPath,
		overlapMatchClassification,
		overlapMatchForPath,
		mergedAgentStateObligations,
	}
	public := []any{
		(OverlapSafetyComponent{}).ValidateBeforeMutation,
		(OverlapSafetyComponent{}).ValidateBeforeMutationWithReport,
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
		_ func([]AgentStateObligation) OverlapSafetyValidation                                                                               = overlapValidationForObligations
		_ func(*OverlapSafetyValidation, AgentStateObligation, types.EntryClass)                                                             = (*OverlapSafetyValidation).passObligation
		_ func(*OverlapSafetyValidation, AgentStateObligation, types.EntryClass, error)                                                      = (*OverlapSafetyValidation).blockObligation
		_ func(*OverlapSafetyValidation, types.Tool, string, types.EntryClass, error)                                                        = (*OverlapSafetyValidation).blockOutcomeAtPath
		_ func(*OverlapSafetyValidation, OverlapSafetyMatch, error)                                                                          = (*OverlapSafetyValidation).ensureBlockedOutcome
		_ func(AgentStateRevalidationOutcome) string                                                                                         = revalidationOutcomeKey
		_ func(*OverlapSafetyRefusal) string                                                                                                 = overlapRefusalBlockingPath
		_ func([]OverlapSafetyMatch, types.Tool, string) types.EntryClass                                                                    = overlapMatchClassification
		_ func([]OverlapSafetyMatch, types.Tool, string) OverlapSafetyMatch                                                                  = overlapMatchForPath
		_ func([]AgentStateObligation, []AgentStateObligation) ([]AgentStateObligation, error)                                               = mergedAgentStateObligations
		_ func(OverlapSafetyComponent, context.Context, OverlapSafetyEvidence, AgentStateRevalidatorLookup) error                            = (OverlapSafetyComponent).ValidateBeforeMutation
		_ func(OverlapSafetyComponent, context.Context, OverlapSafetyEvidence, AgentStateRevalidatorLookup) (OverlapSafetyValidation, error) = (OverlapSafetyComponent).ValidateBeforeMutationWithReport
	)
}
