package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanJSONReceiptSuccessIsVersionedRedactedAndPhysicallyAccounted(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "secret-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "secret payload")

	stdout, stderr, err := runCleanJSONProcess(
		t,
		binary,
		home,
		"clean", "--json", "--force", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err != nil {
		t.Fatalf("JSON cleanup failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful JSON cleanup stderr = %q", stderr)
	}
	if strings.Contains(stdout, home) {
		t.Fatalf("redacted receipt contains home path:\n%s", stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["schema_version"] != float64(1) || document["document_type"] != "clean_receipt" || document["mode"] != "execute" {
		t.Fatalf("receipt header = %+v; want versioned execute receipt", document)
	}
	if document["status"] != "succeeded" {
		t.Fatalf("receipt status = %v; want succeeded", document["status"])
	}
	plan := jsonReceiptObject(t, document, "plan")
	if plan["document_type"] != "clean_plan" || plan["paths_included"] != false {
		t.Fatalf("embedded plan = %+v; want redacted clean plan", plan)
	}
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt(totals, "requested") != 1 ||
		jsonReceiptInt(totals, "removed") != 1 ||
		jsonReceiptInt(totals, "partial") != 0 ||
		jsonReceiptInt(totals, "failed") != 0 ||
		jsonReceiptInt(totals, "cancelled") != 0 {
		t.Fatalf("receipt totals = %+v; want one removed request", totals)
	}
	if jsonReceiptInt64(totals, "freed_bytes") <= 0 {
		t.Fatalf("receipt freed_bytes = %+v; want positive physical-owner bytes", totals)
	}
	targets := jsonReceiptArray(t, document, "physical_targets")
	if len(targets) != 1 {
		t.Fatalf("receipt physical_targets = %d; want one", len(targets))
	}
	target := targets[0]
	if target["id"] != "target-1" || target["state"] != "removed" || target["physical_removed"] != true {
		t.Fatalf("receipt target = %+v; want target-1 removed with absent owner", target)
	}
	if _, ok := target["path"]; ok {
		t.Fatalf("redacted receipt target unexpectedly contains path: %+v", target)
	}
	if _, err := os.Lstat(modules); !os.IsNotExist(err) {
		t.Fatalf("cleanup target still exists: %v", err)
	}
}

func TestCleanJSONReceiptInteractiveIsSilentAndDeterministic(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "interactive-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "interactive")

	commandOutput, commandError, err := runCleanJSONProcessWithInput(
		t,
		binary,
		home,
		"y\n",
		"clean", "--json", "--interactive", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err != nil || commandError != "" {
		t.Fatalf("interactive JSON cleanup = err %v stderr %q stdout %s", err, commandError, commandOutput)
	}
	document := decodeJSONReceiptDocument(t, commandOutput)
	if document["status"] != "succeeded" {
		t.Fatalf("interactive receipt status = %v; want succeeded", document["status"])
	}
	if strings.Contains(commandOutput, "Remove?") || strings.Contains(commandOutput, "removing") || strings.Contains(commandOutput, "Proceed?") {
		t.Fatalf("interactive JSON leaked prompt/progress text:\n%s", commandOutput)
	}
	if _, err := os.Lstat(modules); !os.IsNotExist(err) {
		t.Fatalf("interactive JSON target still exists: %v", err)
	}
}

func TestCleanJSONReceiptInteractiveNoIsAStableNonRequest(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "skip-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "skip")

	stdout, stderr, err := runCleanJSONProcessWithInput(
		t,
		binary,
		home,
		"n\n",
		"clean", "--json", "--interactive", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err != nil || stderr != "" {
		t.Fatalf("interactive no = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["status"] != "succeeded" {
		t.Fatalf("interactive no receipt status = %v; want succeeded", document["status"])
	}
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt(totals, "requested") != 0 || jsonReceiptInt(totals, "skipped") != 1 ||
		jsonReceiptInt(totals, "removed") != 0 {
		t.Fatalf("interactive no totals = %+v; want one non-requested skip", totals)
	}
	if _, err := os.Lstat(modules); err != nil {
		t.Fatalf("interactive no changed target: %v", err)
	}
}

func TestCleanJSONReceiptIncludePathsMatchesPlanRedactionOptIn(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "included-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "include")
	resolvedModules, err := filepath.EvalSymlinks(modules)
	if err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCleanJSONProcess(
		t,
		binary,
		home,
		"clean", "--json", "--force", "--include-paths", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err != nil || stderr != "" {
		t.Fatalf("include-paths JSON cleanup = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["paths_included"] != true {
		t.Fatalf("receipt paths_included = %v; want true", document["paths_included"])
	}
	if !strings.Contains(stdout, resolvedModules) || !strings.Contains(stdout, "included-project") {
		t.Fatalf("include-paths receipt omitted explicit path/project:\n%s", stdout)
	}
	targets := jsonReceiptArray(t, document, "physical_targets")
	if len(targets) != 1 || targets[0]["path"] != resolvedModules {
		t.Fatalf("include-paths physical target = %+v; want %q", targets, resolvedModules)
	}
}

func TestCleanJSONReceiptConfirmationCancellationIsNonZeroAndPathFree(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "cancel-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "cancel")

	stdout, stderr, err := runCleanJSONProcessWithInput(
		t,
		binary,
		home,
		"n\n",
		"clean", "--json", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err == nil {
		t.Fatalf("confirmation cancellation unexpectedly succeeded: stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stderr, home) || strings.Contains(stderr, modules) {
		t.Fatalf("cancellation stderr leaked path: %q", stderr)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["status"] != "cancelled" {
		t.Fatalf("cancelled receipt status = %v; want cancelled", document["status"])
	}
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt(totals, "requested") != 1 || jsonReceiptInt(totals, "cancelled") != 1 ||
		jsonReceiptInt(totals, "removed") != 0 || jsonReceiptInt64(totals, "freed_bytes") != 0 {
		t.Fatalf("cancelled receipt totals = %+v; want one cancelled request and zero freed", totals)
	}
	if _, err := os.Lstat(modules); err != nil {
		t.Fatalf("cancelled cleanup changed target: %v", err)
	}
}

func TestCleanJSONReceiptNonInteractiveEOFIsCancelledWithoutMutation(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "eof-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "eof")

	stdout, stderr, err := runCleanJSONProcessWithInput(
		t, binary, home, "",
		"clean", "--json", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err == nil || strings.Contains(stderr, home) {
		t.Fatalf("noninteractive EOF = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["status"] != cleanJSONReceiptCancelled {
		t.Fatalf("EOF receipt status = %v; want cancelled", document["status"])
	}
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt(totals, "requested") != 1 || jsonReceiptInt(totals, "cancelled") != 1 ||
		jsonReceiptInt64(totals, "freed_bytes") != 0 {
		t.Fatalf("EOF receipt totals = %+v", totals)
	}
	if _, statErr := os.Lstat(modules); statErr != nil {
		t.Fatalf("EOF confirmation mutated target: %v", statErr)
	}
}

func TestCleanJSONReceiptInteractiveInvalidInputCancelsWithoutMutation(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "invalid-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "invalid")

	stdout, stderr, err := runCleanJSONProcessWithInput(
		t, binary, home, "maybe\n",
		"clean", "--json", "--interactive", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err == nil || strings.Contains(stderr, home) {
		t.Fatalf("interactive invalid = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	totals := jsonReceiptObject(t, document, "totals")
	if document["status"] != cleanJSONReceiptCancelled || jsonReceiptInt(totals, "requested") != 1 ||
		jsonReceiptInt(totals, "cancelled") != 1 || jsonReceiptInt64(totals, "freed_bytes") != 0 {
		t.Fatalf("interactive invalid receipt = status %v totals %+v", document["status"], totals)
	}
	if _, statErr := os.Lstat(modules); statErr != nil {
		t.Fatalf("interactive invalid mutated target: %v", statErr)
	}
}

func TestCleanJSONReceiptInteractiveMidwayEOFPreservesPartialMutation(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	first := filepath.Join(home, "workspace", "first", "node_modules")
	second := filepath.Join(home, "workspace", "second", "node_modules")
	writeJSONReceiptFixture(t, first, "first")
	writeJSONReceiptFixture(t, second, "second")

	stdout, stderr, err := runCleanJSONProcessWithInput(
		t, binary, home, "y\n",
		"clean", "--json", "--interactive", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err == nil || strings.Contains(stderr, home) {
		t.Fatalf("interactive midway EOF = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	totals := jsonReceiptObject(t, document, "totals")
	if document["status"] != cleanJSONReceiptPartialFailure || jsonReceiptInt(totals, "requested") != 2 ||
		jsonReceiptInt(totals, "removed") != 1 || jsonReceiptInt(totals, "cancelled") != 1 ||
		jsonReceiptInt64(totals, "freed_bytes") <= 0 {
		t.Fatalf("interactive midway EOF receipt = status %v totals %+v", document["status"], totals)
	}
	remaining := 0
	for _, path := range []string{first, second} {
		if _, statErr := os.Lstat(path); statErr == nil {
			remaining++
		}
	}
	if remaining != 1 {
		t.Fatalf("interactive midway EOF remaining owners = %d; want one", remaining)
	}
}

func TestCleanJSONReceiptPartialExecutionAccountsOnlyVerifiedOwners(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command fixture is Unix-specific")
	}
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	goCache := filepath.Join(home, ".cache", "go-build")
	modules := filepath.Join(home, "workspace", "partial-project", "node_modules")
	writeJSONReceiptFixture(t, goCache, "go cache")
	writeJSONReceiptFixture(t, modules, "modules")
	binDir := t.TempDir()
	writeJSONReceiptExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nexit 7\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, err := runCleanJSONProcess(
		t,
		binary,
		home,
		"clean", "--json", "--force", "--no-guide", "--age=1h", "--category=build-cache,node_modules",
	)
	if err == nil {
		t.Fatalf("partial JSON cleanup unexpectedly succeeded: stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stderr, home) || strings.Contains(stderr, goCache) || strings.Contains(stderr, modules) {
		t.Fatalf("partial cleanup stderr leaked path: %q", stderr)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	if document["status"] != "partial_failure" {
		t.Fatalf("partial receipt status = %v; want partial_failure", document["status"])
	}
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt(totals, "requested") !=
		jsonReceiptInt(totals, "removed")+jsonReceiptInt(totals, "partial")+
			jsonReceiptInt(totals, "failed")+jsonReceiptInt(totals, "cancelled") {
		t.Fatalf("partial receipt violates requested accounting: %+v", totals)
	}
	if jsonReceiptInt(totals, "removed") != 1 || jsonReceiptInt(totals, "failed") != 1 || jsonReceiptInt(totals, "partial") != 0 {
		t.Fatalf("partial receipt totals = %+v; want one removed and one failed", totals)
	}
	if _, err := os.Lstat(modules); !os.IsNotExist(err) {
		t.Fatalf("successful partial target still exists: %v", err)
	}
	if _, err := os.Lstat(goCache); err != nil {
		t.Fatalf("failed command target unexpectedly changed: %v", err)
	}
}

func TestCleanJSONReceiptCommandSuccessDoesNotInventFreedBytes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command fixture is Unix-specific")
	}
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	goCache := filepath.Join(home, ".cache", "go-build")
	writeJSONReceiptFixture(t, goCache, "command leaves owner")
	binDir := t.TempDir()
	writeJSONReceiptExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, err := runCleanJSONProcess(
		t,
		binary,
		home,
		"clean", "--json", "--force", "--no-guide", "--age=1h", "--category=build-cache",
	)
	if err != nil || stderr != "" {
		t.Fatalf("command JSON cleanup = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	totals := jsonReceiptObject(t, document, "totals")
	if jsonReceiptInt64(totals, "freed_bytes") != 0 {
		t.Fatalf("command receipt invented freed bytes while owner remains: %+v", totals)
	}
	targets := jsonReceiptArray(t, document, "physical_targets")
	if len(targets) != 1 || targets[0]["physical_removed"] != false {
		t.Fatalf("command physical target = %+v; want retained owner", targets)
	}
	if _, err := os.Lstat(goCache); err != nil {
		t.Fatalf("command cleanup should preserve its physical owner: %v", err)
	}
}

func TestApplyCleanJSONExecutionReceiptUsesCapturedTargetIDsAfterDeletion(t *testing.T) {
	item := types.DebrisInfo{ID: "deleted", Path: filepath.Join(t.TempDir(), "gone")}
	receipt := cleanJSONReceipt{PhysicalTargets: []cleanJSONReceiptPhysicalTarget{{ID: "target-1", State: cleanJSONReceiptPending}}}
	err := applyCleanJSONExecutionReceipt(&receipt, map[string]string{cleanJSONReceiptItemKey(item): "target-1"}, cleanExecutionReceipt{
		Units: []cleanUnitExecutionReceipt{{Target: item, State: cleanExecutionRemoved, PhysicalRemoved: true, FreedBytes: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PhysicalTargets[0].State != string(cleanExecutionRemoved) || !receipt.PhysicalTargets[0].PhysicalRemoved {
		t.Fatalf("captured ID receipt target = %+v", receipt.PhysicalTargets[0])
	}
}

func TestOrderCleanJSONReceiptPreparedTargetsUsesPlanTargetOrderWithoutMutatingInput(t *testing.T) {
	first := types.DebrisInfo{ID: "first", Path: filepath.Join(t.TempDir(), "first")}
	second := types.DebrisInfo{ID: "second", Path: filepath.Join(t.TempDir(), "second")}
	prepared := []preparedCleanTarget{{Item: second}, {Item: first}}
	ids := map[string]string{
		cleanJSONReceiptItemKey(first):  "target-1",
		cleanJSONReceiptItemKey(second): "target-2",
	}
	ordered, err := orderCleanJSONReceiptPreparedTargets(prepared, ids)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Item.ID != "first" || ordered[1].Item.ID != "second" {
		t.Fatalf("ordered prepared targets = %+v", ordered)
	}
	if prepared[0].Item.ID != "second" || prepared[1].Item.ID != "first" {
		t.Fatalf("ordering mutated caller slice: %+v", prepared)
	}
}

func TestApplyCleanJSONExecutionReceiptIDMissFailsReceiptInvariant(t *testing.T) {
	item := types.DebrisInfo{ID: "unknown", Path: filepath.Join(t.TempDir(), "unknown")}
	receipt := cleanJSONReceipt{PhysicalTargets: []cleanJSONReceiptPhysicalTarget{{ID: "target-1", State: cleanJSONReceiptPending}}}
	applyErr := applyCleanJSONExecutionReceipt(&receipt, nil, cleanExecutionReceipt{
		Units: []cleanUnitExecutionReceipt{{Target: item, State: cleanExecutionRemoved, PhysicalRemoved: true, FreedBytes: 10}},
	})
	if applyErr == nil || !strings.Contains(applyErr.Error(), "execution receipt invariant") {
		t.Fatalf("ID miss error = %v", applyErr)
	}
	finalized, finalizeErr := finishCleanJSONReceipt(receipt, applyErr)
	if finalizeErr == nil || finalized.Status == cleanJSONReceiptSucceeded || finalized.Totals.Failed != 1 {
		t.Fatalf("ID miss final receipt = %+v error=%v", finalized, finalizeErr)
	}
}

func TestFinalizeCleanJSONReceiptRejectsRequestedOutcomeMismatch(t *testing.T) {
	receipt := cleanJSONReceipt{PhysicalTargets: []cleanJSONReceiptPhysicalTarget{{
		ID: "target-1", State: cleanJSONReceiptSkipped, Requested: true,
	}}}
	finalized, err := finalizeCleanJSONReceipt(receipt)
	if err == nil || finalized.Status != cleanJSONReceiptFailed || finalized.Totals.Requested != 1 {
		t.Fatalf("mismatched receipt = %+v error=%v", finalized, err)
	}
}

func TestExecuteCleanJSONReceiptRejectsSelectedPreparedSetMismatchBeforeInteractiveMutation(t *testing.T) {
	root := t.TempDir()
	first := types.DebrisInfo{ID: "refused", Path: filepath.Join(root, "first"), Size: 10}
	second := types.DebrisInfo{ID: "prepared", Path: filepath.Join(root, "second"), Size: 20}
	for _, path := range []string{first.Path, second.Path} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	firstKey, ok := cleanTargetPathKey(first.Path)
	if !ok {
		t.Fatal("first path did not canonicalize")
	}
	secondKey, ok := cleanTargetPathKey(second.Path)
	if !ok {
		t.Fatal("second path did not canonicalize")
	}
	document := cleanJSONPlan{PhysicalTargets: []cleanJSONPhysicalTarget{
		{ID: "target-1", Decision: cleanJSONDecisionSelected, Bytes: first.Size},
		{ID: "target-2", Decision: cleanJSONDecisionSelected, Bytes: second.Size},
	}}
	components := []cleanJSONSnapshotComponent{
		{Key: firstKey, Owner: first},
		{Key: secondKey, Owner: second},
	}
	plan := UnifiedCleanupPlan{Components: []CleanupPhysicalComponent{
		{Owner: first, Selection: CleanupPlanSelected},
		{Owner: second, Selection: CleanupPlanSelected},
	}}

	// The first selected target has been refused at deletion time, leaving
	// only the second target prepared. Interactive input must never be read or
	// applied to that shifted set.
	receipt, err := executeCleanJSONReceipt(
		context.Background(), document, components, plan,
		[]preparedCleanTarget{{Item: second}}, false, true,
	)
	if err == nil || !strings.Contains(err.Error(), "selected and prepared physical target IDs differ") {
		t.Fatalf("set mismatch error = %v", err)
	}
	if receipt.Status != cleanJSONReceiptFailed || receipt.Totals.Requested != 2 || receipt.Totals.Failed != 2 ||
		receipt.Totals.FreedBytes != 0 {
		t.Fatalf("set mismatch receipt = %+v", receipt)
	}
	if !slices.Contains(receipt.PhysicalTargets[0].ReasonCodes, "safety_refused") ||
		!slices.Contains(receipt.PhysicalTargets[1].ReasonCodes, "execution_set_mismatch") {
		t.Fatalf("set mismatch reason codes = %+v", receipt.PhysicalTargets)
	}
	if _, statErr := os.Lstat(second.Path); statErr != nil {
		t.Fatalf("set mismatch consumed interactive input or mutated prepared target: %v", statErr)
	}
}

func writeJSONReceiptFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "payload"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func writeJSONReceiptExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runCleanJSONProcessWithInput(t *testing.T, binary, home, input string, args ...string) (string, string, error) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = cliContractEnv(os.Environ(), home)
	command.Stdin = strings.NewReader(input)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func decodeJSONReceiptDocument(t *testing.T, output string) map[string]any {
	t.Helper()
	document, err := decodeOneJSONReceiptDocument(output)
	if err != nil {
		t.Fatalf("receipt stdout is not one JSON document: %v\n%s", err, output)
	}
	return document
}

func decodeOneJSONReceiptDocument(output string) (map[string]any, error) {
	var document map[string]any
	decoder := json.NewDecoder(strings.NewReader(output))
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON documents")
		}
		return nil, fmt.Errorf("trailing JSON data: %w", err)
	}
	return document, nil
}

func TestDecodeOneJSONReceiptDocumentRejectsMalformedTrailingText(t *testing.T) {
	if _, err := decodeOneJSONReceiptDocument(`{"document_type":"clean_receipt"} trailing`); err == nil {
		t.Fatal("malformed trailing text was accepted")
	}
}

func jsonReceiptObject(t *testing.T, document map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := document[key].(map[string]any)
	if !ok {
		t.Fatalf("receipt field %q = %#v; want object", key, document[key])
	}
	return value
}

func jsonReceiptArray(t *testing.T, document map[string]any, key string) []map[string]any {
	t.Helper()
	values, ok := document[key].([]any)
	if !ok {
		t.Fatalf("receipt field %q = %#v; want array", key, document[key])
	}
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("receipt field %q contains %#v; want object", key, value)
		}
		result = append(result, object)
	}
	return result
}

func jsonReceiptInt(object map[string]any, key string) int {
	return int(jsonReceiptInt64(object, key))
}

func jsonReceiptInt64(object map[string]any, key string) int64 {
	value, ok := object[key].(float64)
	if !ok {
		return 0
	}
	return int64(value)
}
