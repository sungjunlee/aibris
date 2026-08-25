//go:build windows

package cleanjson

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestWindowsPathRedaction(t *testing.T) {
	item := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		Project:  "windows-private-project",
		Path:     `C:\Users\fixture\windows-private-project\node_modules`,
		Size:     7,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	path, ok := cleaner.TargetPathKey(item.Path)
	if !ok {
		path = item.Path
	}
	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: []types.DebrisInfo{item}},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: time.Hour},
		Plan:   selectedPlan(item, "classic_eligible"),
		Audit: []AuditComponent{{
			CanonicalPath: path, Owner: item,
			LogicalRows: []AuditRow{{Item: item, CanonicalPath: path, Relation: overlapOwner}},
		}},
		Inventory: []types.DebrisInfo{item},
	})
	var output bytes.Buffer
	if err := Encode(&output, document); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `C:\Users\fixture`) || strings.Contains(output.String(), item.Project) {
		t.Fatalf("Windows redacted JSON leaked private path data: %s", output.String())
	}
}
