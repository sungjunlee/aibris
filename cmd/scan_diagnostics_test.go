package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPrintJSON_DiagnosticsExperimental(t *testing.T) {
	result := &types.ScanResult{
		ByCategory: map[types.Category]types.CategorySummary{},
		ByTool:     map[types.Tool]types.ToolSummary{},
		Diagnostics: []types.ProviderDiagnostic{
			{
				Tool:     types.ToolCodex,
				State:    types.ScanProgressDone,
				Count:    3,
				Bytes:    4096,
				Duration: 250 * time.Millisecond,
			},
			{
				Tool:     types.ToolClaude,
				State:    types.ScanProgressError,
				Duration: 40 * time.Millisecond,
				Err:      "permission denied",
			},
		},
	}

	output := captureOutput(func() {
		printJSON(result)
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if out.SchemaVersion != scanJSONSchemaVersion {
		t.Errorf("SchemaVersion = %d; want %d", out.SchemaVersion, scanJSONSchemaVersion)
	}
	want := []jsonProviderDiagnostic{
		{Tool: "codex", State: "done", Count: 3, Bytes: 4096, DurationMS: 250},
		{Tool: "claude", State: "error", DurationMS: 40, Error: "permission denied"},
	}
	if !reflect.DeepEqual(out.Diagnostics, want) {
		t.Errorf("Diagnostics = %+v; want %+v", out.Diagnostics, want)
	}
}

func TestScanCmd_JSONDiagnosticsOptIn(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "app", "node_modules", "pkg"), 0755); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", workspace, "--json", "--diagnostics"})
		rootCmd.Execute()
	})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &raw); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	diagRaw, ok := raw["diagnostics"]
	if !ok {
		t.Fatalf("--diagnostics output missing diagnostics array; got: %s", output)
	}
	var diagnostics []jsonProviderDiagnostic
	if err := json.Unmarshal(diagRaw, &diagnostics); err != nil {
		t.Fatalf("invalid diagnostics array: %v", err)
	}
	if len(diagnostics) == 0 {
		t.Fatal("diagnostics array is empty; want one entry per provider")
	}
	var nodeModules *jsonProviderDiagnostic
	for i := range diagnostics {
		if diagnostics[i].State != "done" && diagnostics[i].State != "error" {
			t.Errorf("diagnostic state = %q; want done or error", diagnostics[i].State)
		}
		if diagnostics[i].Tool == string(types.ToolNodeModules) {
			nodeModules = &diagnostics[i]
		}
	}
	if nodeModules == nil {
		t.Fatalf("diagnostics missing %s provider; got: %s", types.ToolNodeModules, diagRaw)
	}
	if nodeModules.State != "done" || nodeModules.Count != 1 {
		t.Errorf("node_modules diagnostic = %+v; want done with 1 item", nodeModules)
	}
}

func TestScanCmd_JSONOmitsDiagnosticsWithoutFlag(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json"})
		rootCmd.Execute()
	})

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(output), &raw); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if _, ok := raw["diagnostics"]; ok {
		t.Errorf("diagnostics must be absent without --diagnostics; got: %s", raw["diagnostics"])
	}
}

func TestScanCmd_HumanDiagnosticsOptIn(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)

	withFlag := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--diagnostics"})
		rootCmd.Execute()
	})
	if !strings.Contains(withFlag, "diagnostics (experimental)") {
		t.Errorf("--diagnostics output missing diagnostics section; got: %s", withFlag)
	}
	if !strings.Contains(withFlag, string(types.ToolNodeModules)) {
		t.Errorf("--diagnostics output missing provider names; got: %s", withFlag)
	}

	resetScanFlags()
	withoutFlag := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	if strings.Contains(withoutFlag, "diagnostics (experimental)") {
		t.Errorf("plain scan output must stay concise; got: %s", withoutFlag)
	}
}
