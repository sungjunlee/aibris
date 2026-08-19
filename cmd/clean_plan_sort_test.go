package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestClassicDryRunTargetsPrintLargestFirst(t *testing.T) {
	small := types.DebrisInfo{
		ID: "uv-small", Tool: types.ToolPipCache, Category: types.CategoryOtherCache,
		Path: "/tmp/uv", Size: 1700, Reason: "old cache",
	}
	large := types.DebrisInfo{
		ID: "dart-large", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
		Path: "/tmp/dart", Size: 4300, Reason: "old cache",
	}
	mid := types.DebrisInfo{
		ID: "agent-mid", Tool: types.ToolClaude, Category: types.CategoryAgentState,
		Path: "/tmp/agent", Size: 2000, Status: types.WorktreeOrphaned, Reason: "orphaned",
	}
	targets := []types.DebrisInfo{small, large, mid}

	output := captureOutput(func() {
		printCleanPlan(targets, cleanPlanModeDryRun)
	})

	if targets[0].ID != "uv-small" || targets[1].ID != "dart-large" || targets[2].ID != "agent-mid" {
		t.Fatalf("print mutated execution slice: %+v", idsOf(targets))
	}

	section := dryRunTargetsSection(t, output)
	got := targetNameOrder(section, []string{"uv-small", "dart-large", "agent-mid"})
	want := []string{"dart-large", "agent-mid", "uv-small"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("dry-run print order = %v; want %v\n%s", got, want, section)
	}
}

func TestClassicDeletePlanKeepsInputOrder(t *testing.T) {
	targets := []types.DebrisInfo{
		{ID: "first", Size: 100, Category: types.CategoryNodeModules, Path: "/tmp/a"},
		{ID: "second", Size: 900, Category: types.CategoryNodeModules, Path: "/tmp/b"},
		{ID: "third", Size: 400, Category: types.CategoryNodeModules, Path: "/tmp/c"},
	}
	output := captureOutput(func() {
		printCleanPlan(targets, cleanPlanModeDelete)
	})
	section := dryRunTargetsSection(t, output)
	got := targetNameOrder(section, []string{"first", "second", "third"})
	if strings.Join(got, ",") != "first,second,third" {
		t.Fatalf("delete plan order = %v; want input order\n%s", got, section)
	}
}

func dryRunTargetsSection(t *testing.T, output string) string {
	t.Helper()
	idx := strings.Index(output, "\ntargets\n")
	if idx < 0 {
		t.Fatalf("missing targets table:\n%s", output)
	}
	return output[idx:]
}

func targetNameOrder(section string, names []string) []string {
	type hit struct {
		name string
		pos  int
	}
	hits := make([]hit, 0, len(names))
	for _, name := range names {
		if pos := strings.Index(section, name); pos >= 0 {
			hits = append(hits, hit{name: name, pos: pos})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	out := make([]string, len(hits))
	for i, hit := range hits {
		out[i] = hit.name
	}
	return out
}

func idsOf(targets []types.DebrisInfo) []string {
	ids := make([]string, len(targets))
	for i, target := range targets {
		ids[i] = target.ID
	}
	return ids
}
