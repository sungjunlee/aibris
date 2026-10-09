package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestGoCacheUnverifiedCLI(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := testutil.GoBuildCache(home)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := "personal data"
	if err := os.WriteFile(filepath.Join(path, "personal.txt"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		stdout, stderr, err := runCleanJSONProcess(t, binary, home, args...)
		if err != nil {
			t.Fatalf("%v = %v stdout=%s stderr=%s", args, err, stdout, stderr)
		}
		return stdout
	}
	livePreview := run("clean", "--no-guide", "--dry-run", "--root", path, "--age=1ns", "--pressure")
	if !strings.Contains(livePreview, "foreign entry") || !strings.Contains(livePreview, "personal.txt") {
		t.Fatalf("live human preview lacks cause: %s", livePreview)
	}
	scan := run("scan", "--json", "--root", path)
	document := decodeJSONReceiptDocument(t, scan)
	items := jsonReceiptArray(t, document, "items")
	if len(items) != 1 || items[0]["id"] != "go-build" || items[0]["tool"] != "build-cache" ||
		!strings.Contains(items[0]["reason"].(string), "foreign entry") || jsonReceiptInt64(items[0], "size") != int64(len(payload)) {
		t.Fatalf("unverified scan row = %s", scan)
	}
	if summary := jsonReceiptObject(t, document, "summary"); jsonReceiptInt64(summary, "total_size") != int64(len(payload)) {
		t.Fatalf("unverified bytes absent from totals: %s", scan)
	}
	// The scan above persists the row; both previews and real --force cleanup
	// must recheck the live catalog even when replaying that inventory.
	for _, flags := range [][]string{{"--dry-run"}, {"--dry-run", "--pressure"}, {"--force", "--pressure"}} {
		// Each selector gets a fresh snapshot, so pressure changes do not
		// invalidate cache reuse before the live eligibility check is exercised.
		run("scan", "--json", "--root", path)
		args := append([]string{"clean", "--no-guide", "--json", "--root", path, "--age=1ns"}, flags...)
		plan := run(args...)
		doc := decodeJSONReceiptDocument(t, plan)
		if _, ok := doc["plan"]; ok {
			doc = jsonReceiptObject(t, doc, "plan")
		}
		if evidence := jsonReceiptObject(t, doc, "evidence"); evidence["source"] != "cached" {
			t.Fatalf("expected scan-cache replay: %s", plan)
		}
		rows := jsonReceiptArray(t, doc, "rows")
		targets := jsonReceiptArray(t, doc, "physical_targets")
		totals := jsonReceiptObject(t, doc, "totals")
		if len(rows) != 1 || len(targets) != 1 || rows[0]["policy_decision"] != "skipped" ||
			rows[0]["decision"] != "skipped" || targets[0]["decision"] != "skipped" ||
			!strings.Contains(plan, "go_cache_unverified") || jsonReceiptInt64(totals, "selected_bytes") != 0 ||
			jsonReceiptInt(totals, "selected") != 0 {
			t.Fatalf("unverified cleanup = %s", plan)
		}
	}
	for _, args := range [][]string{
		{"scan", "--root", path},
		{"clean", "--no-guide", "--dry-run", "--root", path, "--age=1ns", "--pressure"},
	} {
		output := run(args...)
		if !strings.Contains(output, "foreign entry") || !strings.Contains(output, "personal.txt") {
			t.Errorf("human output lacks cause: %s", output)
		}
	}
	if data, err := os.ReadFile(filepath.Join(path, "personal.txt")); err != nil || string(data) != payload {
		t.Fatalf("unverified cache mutated: %q, %v", data, err)
	}
}
