//go:build windows

package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanJSONWindowsPathRedaction(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	item := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		Project:  "windows-private-project",
		Path:     `C:\Users\fixture\windows-private-project\node_modules`,
		Size:     7,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	physical, _ := cleanAuditPhysicalComponents([]types.DebrisInfo{item}, nil)
	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: []types.DebrisInfo{item}},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour},
		nil,
		[]types.DebrisInfo{item},
		nil,
		cleanAudit{Components: physical},
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := encodeCleanJSON(&output, document); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `C:\Users\fixture`) || strings.Contains(output.String(), item.Project) {
		t.Fatalf("Windows redacted JSON leaked private path data: %s", output.String())
	}
}
