package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanCmd_GuidedReceiptFileEmitsVersionedExecutionReceipt(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	recommended := saveGuidedReceiptCleanFixture(t, home, "guided-receipt", 1)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	defer withStdin(t, "\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--guide", "--force", "--receipt-file", receiptPath})
		rootCmd.Execute()
	})

	if !strings.Contains(output, "cleanup receipt") || !strings.Contains(output, "removed    1 item") {
		t.Fatalf("guided execution lost its human receipt block: %s", output)
	}
	if _, err := os.Lstat(recommended[0]); !os.IsNotExist(err) {
		t.Fatalf("guided execution did not remove the recommended worktree: %v", err)
	}
	document := decodeCleanReceiptFile(t, receiptPath)
	if document["schema_version"] != float64(cleanJSONSchemaVersion) ||
		document["document_type"] != "clean_receipt" ||
		document["mode"] != "execute" {
		t.Fatalf("guided receipt header = %+v; want a versioned execute receipt", document)
	}
	if document["status"] != cleanJSONReceiptSucceeded {
		t.Fatalf("guided receipt status = %v; want succeeded", document["status"])
	}
	plan := jsonReceiptObject(t, document, "plan")
	if plan["document_type"] != "clean_plan" || plan["mode"] != "dry_run" {
		t.Fatalf("embedded plan = %+v; want the documented clean_plan/dry_run pair", plan)
	}
	totals := jsonReceiptObject(t, document, "totals")
	assertCleanReceiptRequestAccounting(t, totals)
	if jsonReceiptInt(totals, "requested") != 1 || jsonReceiptInt(totals, "removed") != 1 {
		t.Fatalf("guided receipt totals = %+v; want one removed request", totals)
	}
}

// TestCleanCmd_GuidedReceiptFileFollowsAcceptedSelection proves the receipt is
// rendered from the plan the guided review accepted. Rebuilding it from the
// default candidate set would leave the de-selected worktree requested.
func TestCleanCmd_GuidedReceiptFileFollowsAcceptedSelection(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	recommended := saveGuidedReceiptCleanFixture(t, home, "guided-deselect", 2)
	canonical := resolveCleanFixturePaths(t, recommended)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	// Toggle the first recommended row off, then accept the review.
	defer withStdin(t, "1\n\n")()

	captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--guide", "--force", "--include-paths", "--receipt-file", receiptPath,
		})
		rootCmd.Execute()
	})

	kept, deleted := splitSurvivingCleanFixturePaths(recommended, canonical)
	if len(kept) != 1 || len(deleted) != 1 {
		t.Fatalf("guided de-selection removed %d of %d recommended worktrees; want exactly one", len(deleted), len(recommended))
	}
	document := decodeCleanReceiptFile(t, receiptPath)
	targets := cleanReceiptTargetsByPath(t, document)
	keptTarget, ok := targets[kept[0]]
	if !ok {
		t.Fatalf("de-selected worktree missing from receipt targets %v", targets)
	}
	if keptTarget["requested"] != false || keptTarget["state"] == string(cleanExecutionRemoved) {
		t.Fatalf("de-selected worktree target = %+v; want a non-requested, non-removed target", keptTarget)
	}
	deletedTarget, ok := targets[deleted[0]]
	if !ok {
		t.Fatalf("accepted worktree missing from receipt targets %v", targets)
	}
	if deletedTarget["requested"] != true || deletedTarget["state"] != string(cleanExecutionRemoved) {
		t.Fatalf("accepted worktree target = %+v; want a requested removal", deletedTarget)
	}
	totals := jsonReceiptObject(t, document, "totals")
	assertCleanReceiptRequestAccounting(t, totals)
	if jsonReceiptInt(totals, "requested") != 1 || jsonReceiptInt(totals, "removed") != 1 {
		t.Fatalf("de-selected guided receipt totals = %+v; want one removed request", totals)
	}
	if document["status"] != cleanJSONReceiptSucceeded {
		t.Fatalf("de-selected guided receipt status = %v; want succeeded", document["status"])
	}
}

func TestCleanCmd_GuidedReceiptFileUsesJSONRouteRedaction(t *testing.T) {
	for _, tt := range []struct {
		name         string
		args         []string
		wantIncluded bool
	}{
		{name: "redacted", wantIncluded: false},
		{name: "include paths", args: []string{"--include-paths"}, wantIncluded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			home := t.TempDir()
			testutil.SetHome(t, home)
			recommended := saveGuidedReceiptCleanFixture(t, home, "guided-redaction", 1)
			resolvedTarget := resolveCleanFixturePaths(t, recommended)[0]
			resolvedHome, err := filepath.EvalSymlinks(home)
			if err != nil {
				t.Fatal(err)
			}
			receiptPath := filepath.Join(t.TempDir(), "receipt.json")
			defer withStdin(t, "\n")()

			args := append([]string{"clean", "--guide", "--force"}, tt.args...)
			captureOutput(func() {
				rootCmd.SetArgs(append(args, "--receipt-file", receiptPath))
				rootCmd.Execute()
			})

			raw, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatalf("reading receipt file: %v", err)
			}
			document := decodeCleanReceiptFile(t, receiptPath)
			if document["paths_included"] != tt.wantIncluded {
				t.Fatalf("receipt paths_included = %v; want %v", document["paths_included"], tt.wantIncluded)
			}
			if tt.wantIncluded {
				if !strings.Contains(string(raw), resolvedTarget) {
					t.Fatalf("include-paths receipt omitted the executed path %q:\n%s", resolvedTarget, raw)
				}
				return
			}
			if strings.Contains(string(raw), resolvedHome) || strings.Contains(string(raw), home) {
				t.Fatalf("redacted receipt leaked an absolute path:\n%s", raw)
			}
			if strings.Contains(string(raw), "project") {
				t.Fatalf("redacted receipt leaked a project label:\n%s", raw)
			}
		})
	}
}

func TestCleanCmd_GuidedReceiptFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode bits are not meaningful on Windows")
	}
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	saveGuidedReceiptCleanFixture(t, home, "guided-receipt-mode", 1)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	// A pre-existing group/world-readable sink is the case an open mode alone
	// does not cover: the mode argument applies only when the call creates the
	// file, so the document would land in a file anyone could read.
	if err := os.WriteFile(receiptPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	defer withStdin(t, "\n")()

	captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--guide", "--force", "--include-paths", "--receipt-file", receiptPath})
		rootCmd.Execute()
	})

	info, err := os.Stat(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("receipt file mode = %v; want exactly 0600", perm)
	}
}

func TestCleanReceiptFileRejectsDryRunWithoutWritingAFile(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "human", args: []string{"clean", "--dry-run"}},
		{name: "json", args: []string{"clean", "--dry-run", "--json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			receiptPath := filepath.Join(t.TempDir(), "receipt.json")
			stdout, stderr, err := runCleanJSONProcess(t, binary, home,
				append(tt.args, "--receipt-file", receiptPath)...)
			if err == nil {
				t.Fatalf("dry-run receipt request succeeded: stdout=%q stderr=%q", stdout, stderr)
			}
			if stdout != "" || stderr != "error: --receipt-file requires an execution run (remove --dry-run)\n" {
				t.Fatalf("dry-run receipt refusal = stdout %q stderr %q", stdout, stderr)
			}
			if _, statErr := os.Stat(receiptPath); !os.IsNotExist(statErr) {
				t.Fatalf("refused dry-run wrote a receipt file: %v", statErr)
			}
		})
	}
}

func TestCleanReceiptFileRejectsClassicRouteWithoutDeleting(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, tt := range []struct {
		name string
		args []string
	}{
		// Refused before the scan.
		{name: "no guide", args: []string{"--no-guide"}},
		// Refused after the scan settles the route on classic.
		{name: "classic selector", args: []string{"--category=node_modules"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			modules := filepath.Join(home, "workspace", "classic-receipt", "node_modules")
			writeJSONReceiptFixture(t, modules, "classic")
			receiptPath := filepath.Join(t.TempDir(), "receipt.json")

			args := append([]string{"clean", "--force", "--age=1h"}, tt.args...)
			_, stderr, err := runCleanJSONProcess(t, binary, home,
				append(args, "--receipt-file", receiptPath)...)
			if err == nil {
				t.Fatalf("classic receipt request succeeded: stderr=%q", stderr)
			}
			if !strings.Contains(stderr, "--receipt-file is not available on the classic route; use --json for a classic receipt") {
				t.Fatalf("classic receipt refusal stderr = %q", stderr)
			}
			if _, statErr := os.Stat(receiptPath); !os.IsNotExist(statErr) {
				t.Fatalf("refused classic route wrote a receipt file: %v", statErr)
			}
			if _, statErr := os.Lstat(modules); statErr != nil {
				t.Fatalf("refused classic route deleted its candidate: %v", statErr)
			}
		})
	}
}

func TestCleanJSONReceiptFileMatchesStdoutReceipt(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "json-receipt-file", "node_modules")
	writeJSONReceiptFixture(t, modules, "json receipt file")
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--json", "--force", "--no-guide", "--age=1h", "--category=node_modules",
		"--receipt-file", receiptPath)
	if err != nil || stderr != "" {
		t.Fatalf("JSON receipt-file cleanup = err %v stderr %q stdout %s", err, stderr, stdout)
	}
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("reading receipt file: %v", err)
	}
	if string(raw) != stdout {
		t.Fatalf("receipt file differs from stdout receipt:\nfile:\n%s\nstdout:\n%s", raw, stdout)
	}
	document := decodeCleanReceiptFile(t, receiptPath)
	if document["status"] != cleanJSONReceiptSucceeded {
		t.Fatalf("JSON receipt-file status = %v; want succeeded", document["status"])
	}
	if _, statErr := os.Lstat(modules); !os.IsNotExist(statErr) {
		t.Fatalf("JSON receipt-file cleanup did not remove its target: %v", statErr)
	}
}

// TestCleanJSONReceiptFileDoesNotReopenGuidedJSONExecution guards the #202
// refusal: a receipt sink never turns --json --guide into an execution route.
func TestCleanJSONReceiptFileDoesNotReopenGuidedJSONExecution(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "guide-json-receipt", "node_modules")
	writeJSONReceiptFixture(t, modules, "guided json")
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	for _, args := range [][]string{
		{"clean", "--json", "--guide", "--force"},
		{"clean", "--json", "--guide", "--interactive"},
	} {
		stdout, stderr, err := runCleanJSONProcess(t, binary, home,
			append(args, "--receipt-file", receiptPath)...)
		if err == nil {
			t.Fatalf("guided JSON execution succeeded: stdout=%q stderr=%q", stdout, stderr)
		}
		if stdout != "" || stderr != "error: non-dry-run --json cannot use --guide\n" {
			t.Fatalf("guided JSON execution refusal = stdout %q stderr %q", stdout, stderr)
		}
		if _, statErr := os.Stat(receiptPath); !os.IsNotExist(statErr) {
			t.Fatalf("refused guided JSON execution wrote a receipt file: %v", statErr)
		}
		if _, statErr := os.Lstat(modules); statErr != nil {
			t.Fatalf("refused guided JSON execution mutated its candidate: %v", statErr)
		}
	}
}

func TestCleanCmd_GuidedReceiptFileIsNotWrittenWithoutExecution(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	recommended := saveGuidedReceiptCleanFixture(t, home, "guided-declined", 1)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	// Accept the guided review, then leave the execution confirmation unanswered.
	defer withStdin(t, "\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--guide", "--receipt-file", receiptPath})
		rootCmd.Execute()
	})

	if !strings.Contains(output, "No confirmation received") {
		t.Fatalf("guided run without confirmation output = %s", output)
	}
	if _, err := os.Lstat(recommended[0]); err != nil {
		t.Fatalf("declined confirmation removed the recommended worktree: %v", err)
	}
	if _, err := os.Stat(receiptPath); !os.IsNotExist(err) {
		t.Fatalf("a run that executed nothing wrote a receipt file: %v", err)
	}
}

// TestCleanCmd_GuidedInteractiveDeclineKeepsRunStatus pins the contract this
// flag lives under: --receipt-file is an observability sink. The same guided
// interactive run — one target declined, one removed — must produce the same
// exit status and the same stdout with and without the flag.
func TestCleanCmd_GuidedInteractiveDeclineKeepsRunStatus(t *testing.T) {
	binary := buildCLIContractBinary(t)
	resetCleanFlags()
	// Accept the guided review, decline the first target, remove the second.
	answers := []guidedCleanPromptAnswer{
		{prompt: "q to abort: ", reply: "\n"},
		{prompt: "Remove? [y/N]: ", reply: "n\n"},
		{prompt: "Remove? [y/N]: ", reply: "y\n"},
	}

	observed := t.TempDir()
	testutil.SetHome(t, observed)
	recommended := saveGuidedReceiptCleanFixture(t, observed, "guided-decline", 2)
	canonical := resolveCleanFixturePaths(t, recommended)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")
	stdout, stderr, err := runGuidedCleanWithAnswers(t, binary, observed, answers,
		"clean", "--guide", "--interactive", "--include-paths", "--receipt-file", receiptPath)

	baselineHome := t.TempDir()
	testutil.SetHome(t, baselineHome)
	saveGuidedReceiptCleanFixture(t, baselineHome, "guided-decline", 2)
	baseStdout, baseStderr, baseErr := runGuidedCleanWithAnswers(t, binary, baselineHome, answers,
		"clean", "--guide", "--interactive")

	if err != nil || baseErr != nil {
		t.Fatalf("declined interactive run exit status: with receipt file %v (stderr %q), without %v (stderr %q)",
			err, stderr, baseErr, baseStderr)
	}
	if normalizeGuidedCleanTranscript(t, stdout, observed) !=
		normalizeGuidedCleanTranscript(t, baseStdout, baselineHome) {
		t.Fatalf("--receipt-file changed stdout:\nwith:\n%s\nwithout:\n%s", stdout, baseStdout)
	}
	if stderr != baseStderr {
		t.Fatalf("--receipt-file changed stderr: with %q, without %q", stderr, baseStderr)
	}

	kept, deleted := splitSurvivingCleanFixturePaths(recommended, canonical)
	if len(kept) != 1 || len(deleted) != 1 {
		t.Fatalf("declined interactive run removed %d of %d targets; want exactly one", len(deleted), len(recommended))
	}
	document := decodeCleanReceiptFile(t, receiptPath)
	if document["status"] != cleanJSONReceiptSucceeded {
		t.Fatalf("declined interactive receipt status = %v; want succeeded", document["status"])
	}
	targets := cleanReceiptTargetsByPath(t, document)
	declined, ok := targets[kept[0]]
	if !ok {
		t.Fatalf("declined target missing from receipt targets %v", targets)
	}
	if declined["state"] != cleanJSONReceiptSkipped || declined["requested"] != false ||
		!strings.Contains(fmt.Sprint(declined["reason_codes"]), "not_confirmed") {
		t.Fatalf("declined target = %+v; want a non-requested skip with not_confirmed", declined)
	}
	removed, ok := targets[deleted[0]]
	if !ok {
		t.Fatalf("removed target missing from receipt targets %v", targets)
	}
	if removed["state"] != string(cleanExecutionRemoved) || removed["requested"] != true {
		t.Fatalf("removed target = %+v; want a requested removal", removed)
	}
	totals := jsonReceiptObject(t, document, "totals")
	assertCleanReceiptRequestAccounting(t, totals)
	if jsonReceiptInt(totals, "requested") != 1 || jsonReceiptInt(totals, "removed") != 1 ||
		jsonReceiptInt(totals, "skipped") != 1 {
		t.Fatalf("declined interactive totals = %+v; want one removed request and one skip", totals)
	}
}

// TestCleanCmd_GuidedInteractiveUnansweredTargetIsCancelled covers the other
// interactive disposition: a confirmation stream that ends before a target is
// answered cancels that request, exactly as --json --interactive reports it.
// Unlike a decline, that is not a completed run, and it keeps the receipt
// route's non-zero exit.
func TestCleanCmd_GuidedInteractiveUnansweredTargetIsCancelled(t *testing.T) {
	binary := buildCLIContractBinary(t)
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	recommended := saveGuidedReceiptCleanFixture(t, home, "guided-unanswered", 1)
	receiptPath := filepath.Join(t.TempDir(), "receipt.json")

	// Accept the guided review, then end the stream before the target prompt.
	_, stderr, err := runCleanJSONProcessWithInput(t, binary, home, "\n",
		"clean", "--guide", "--interactive", "--receipt-file", receiptPath)
	if err == nil {
		t.Fatalf("cancelled interactive confirmation exited zero; stderr=%q", stderr)
	}
	if _, statErr := os.Lstat(recommended[0]); statErr != nil {
		t.Fatalf("unanswered interactive target was removed: %v", statErr)
	}
	document := decodeCleanReceiptFile(t, receiptPath)
	if document["status"] != cleanJSONReceiptCancelled {
		t.Fatalf("unanswered interactive receipt status = %v; want cancelled", document["status"])
	}
	totals := jsonReceiptObject(t, document, "totals")
	assertCleanReceiptRequestAccounting(t, totals)
	if jsonReceiptInt(totals, "cancelled") != 1 || jsonReceiptInt64(totals, "freed_bytes") != 0 {
		t.Fatalf("unanswered interactive totals = %+v; want one cancelled request", totals)
	}
	for _, target := range jsonReceiptArray(t, document, "physical_targets") {
		if target["requested"] != true {
			continue
		}
		if target["state"] != cleanJSONReceiptCancelled ||
			!strings.Contains(fmt.Sprint(target["reason_codes"]), "confirmation_cancelled") {
			t.Fatalf("unanswered target = %+v; want a cancelled request", target)
		}
	}
}

// TestGuidedCleanExecutionReceiptFailsClosedOnUnrecordedTarget keeps the
// fail-closed backstop covered now that both interactive dispositions report
// themselves. A requested target the executor returned no unit for still
// cannot be reported as anything but a failure.
func TestGuidedCleanExecutionReceiptFailsClosedOnUnrecordedTarget(t *testing.T) {
	resetCleanFlags()
	pending := guidedCleanExecutionReceipt{
		receipt: newCleanJSONReceipt(cleanJSONPlan{
			SchemaVersion: cleanJSONSchemaVersion,
			DocumentType:  "clean_plan",
			Mode:          "dry_run",
			PhysicalTargets: []cleanJSONPhysicalTarget{
				unrecordedReceiptTargetFixture("target-1"),
				unrecordedReceiptTargetFixture("target-2"),
			},
		}),
		targetIDs: map[string]string{"key-1": "target-1", "key-2": "target-2"},
	}

	receipt, err := pending.finish(cleanExecutionReceipt{
		Units: []cleanUnitExecutionReceipt{{
			ReceiptTargetKey: "key-1",
			State:            cleanExecutionRemoved,
			PhysicalRemoved:  true,
			FreedBytes:       2048,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("fail-closed receipt broke its accounting: %v", err)
	}
	// One recorded removal beside one unrecorded request is the contract's
	// partial failure; the run can never report success for the missing one.
	if receipt.Status != cleanJSONReceiptPartialFailure {
		t.Fatalf("unrecorded target receipt status = %q; want partial_failure", receipt.Status)
	}
	unrecorded := receipt.PhysicalTargets[1]
	if unrecorded.State != cleanJSONReceiptFailed || !unrecorded.Requested ||
		unrecorded.PhysicalRemoved || unrecorded.FreedBytes != 0 ||
		!strings.Contains(fmt.Sprint(unrecorded.ReasonCodes), "execution_not_recorded") {
		t.Fatalf("unrecorded target = %+v; want a fail-closed unrecorded outcome", unrecorded)
	}
	if receipt.Totals.Requested != receipt.Totals.Removed+receipt.Totals.Failed {
		t.Fatalf("unrecorded target totals = %+v; want every request accounted", receipt.Totals)
	}
}

func unrecordedReceiptTargetFixture(id string) cleanJSONPhysicalTarget {
	return cleanJSONPhysicalTarget{
		ID:          id,
		Decision:    cleanJSONDecisionSelected,
		Bytes:       2048,
		Category:    string(types.CategoryWorktree),
		Tool:        string(types.ToolCodex),
		CleanupKind: string(types.CleanupRemovePath),
	}
}

type guidedCleanPromptAnswer struct {
	prompt string
	reply  string
}

// runGuidedCleanWithAnswers drives one guided cleanup the way an operator
// does: each reply is written only after its prompt is printed. Writing them
// up front would not reach the per-target confirmations, because the guided
// review's own scanner buffers whatever is already in the pipe.
func runGuidedCleanWithAnswers(
	t *testing.T,
	binary, home string,
	answers []guidedCleanPromptAnswer,
	args ...string,
) (string, string, error) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = cliContractEnv(os.Environ(), home)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	watchdog := time.AfterFunc(2*time.Minute, func() { command.Process.Kill() })
	defer watchdog.Stop()

	transcript, answerErr := answerGuidedCleanPrompts(stdout, stdin, answers)
	stdin.Close()
	rest, _ := io.ReadAll(stdout)
	transcript += string(rest)
	runErr := command.Wait()
	if answerErr != nil {
		t.Fatalf("driving guided prompts: %v\nstdout:\n%s\nstderr:\n%s", answerErr, transcript, stderr.String())
	}
	return transcript, stderr.String(), runErr
}

func answerGuidedCleanPrompts(
	stdout io.Reader,
	stdin io.Writer,
	answers []guidedCleanPromptAnswer,
) (string, error) {
	transcript := ""
	consumed := 0
	buffer := make([]byte, 4096)
	for _, answer := range answers {
		for {
			if index := strings.Index(transcript[consumed:], answer.prompt); index >= 0 {
				consumed += index + len(answer.prompt)
				break
			}
			read, err := stdout.Read(buffer)
			transcript += string(buffer[:read])
			if err != nil {
				return transcript, fmt.Errorf("waiting for prompt %q: %w", answer.prompt, err)
			}
		}
		if _, err := io.WriteString(stdin, answer.reply); err != nil {
			return transcript, fmt.Errorf("answering prompt %q: %w", answer.prompt, err)
		}
	}
	return transcript, nil
}

// normalizeGuidedCleanTranscript hides the two fields that cannot match across
// two runs of the same fixture: each run has its own home, and each observes
// its scan cache at a slightly different age. Everything else must be equal.
func normalizeGuidedCleanTranscript(t *testing.T, transcript, home string) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		transcript = strings.ReplaceAll(transcript, resolved, "<home>")
	}
	transcript = strings.ReplaceAll(transcript, home, "<home>")
	return regexp.MustCompile(`\d+[smhd] old`).ReplaceAllString(transcript, "<age> old")
}

// saveGuidedReceiptCleanFixture builds one repository whose newest
// DefaultKeepPerRepository worktrees are held by retention and whose remaining
// older worktrees are recommended, so a guided run has an exact expected
// selection. It returns the recommended worktree paths.
func saveGuidedReceiptCleanFixture(t *testing.T, home, id string, recommended int) []string {
	t.Helper()
	items := make([]types.DebrisInfo, 0, DefaultKeepPerRepository+recommended)
	add := func(fixtureID string, activity time.Time) string {
		path := createCleanCodexGitWorktree(t, home, fixtureID)
		if err := os.Chtimes(path, activity, activity); err != nil {
			t.Fatal(err)
		}
		items = append(items, types.DebrisInfo{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       fixtureID,
			Project:  "project",
			Source:   ".codex",
			Path:     path,
			Size:     2 * DefaultCleanupMinSize,
			ModTime:  activity,
			Status:   types.WorktreeActive,
		})
		return path
	}
	for i := 0; i < DefaultKeepPerRepository; i++ {
		add(fmt.Sprintf("%s-retained-%d", id, i), time.Now().Add(-8*24*time.Hour))
	}
	paths := make([]string, 0, recommended)
	for i := 0; i < recommended; i++ {
		paths = append(paths, add(fmt.Sprintf("%s-old-%d", id, i), time.Now().Add(-30*24*time.Hour)))
	}
	runGitFixture(t, filepath.Join(home, "repositories", "repo"), "reflog", "expire", "--expire=now", "--all")
	saveFreshCodexActivityCacheFixture(t)
	saveCleanCacheFixture(t, home, items)
	return paths
}

// resolveCleanFixturePaths spells fixture paths the way the cleanup plan
// canonicalizes them. It must run before execution: a removed target can no
// longer be resolved.
func resolveCleanFixturePaths(t *testing.T, paths []string) []string {
	t.Helper()
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		resolved = append(resolved, canonical)
	}
	return resolved
}

func splitSurvivingCleanFixturePaths(paths, canonical []string) (kept, deleted []string) {
	for i, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			kept = append(kept, canonical[i])
			continue
		}
		deleted = append(deleted, canonical[i])
	}
	return kept, deleted
}

func decodeCleanReceiptFile(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading receipt file: %v", err)
	}
	return decodeJSONReceiptDocument(t, string(raw))
}

func cleanReceiptTargetsByPath(t *testing.T, document map[string]any) map[string]map[string]any {
	t.Helper()
	targets := make(map[string]map[string]any)
	for _, target := range jsonReceiptArray(t, document, "physical_targets") {
		path, ok := target["path"].(string)
		if !ok {
			t.Fatalf("receipt target has no explicit path: %+v", target)
		}
		targets[path] = target
	}
	return targets
}

func assertCleanReceiptRequestAccounting(t *testing.T, totals map[string]any) {
	t.Helper()
	outcomes := jsonReceiptInt(totals, "removed") + jsonReceiptInt(totals, "partial") +
		jsonReceiptInt(totals, "failed") + jsonReceiptInt(totals, "cancelled")
	if jsonReceiptInt(totals, "requested") != outcomes {
		t.Fatalf("receipt violates requested accounting: %+v", totals)
	}
}

// TestCleanReceiptFileRejectsSinkInsideACleanupTarget refuses a sink the run is
// about to delete. Writing there would recreate the path after its removal, so
// the receipt would report a target as removed while it exists again.
func TestCleanReceiptFileRejectsSinkInsideACleanupTarget(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	writeJSONReceiptFixture(t, modules, "payload")
	receiptPath := filepath.Join(modules, "receipt.json")

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--json", "--force", "--age", "1h",
		"--category", "node_modules", "--receipt-file", receiptPath)
	if err == nil {
		t.Fatalf("overlapping receipt sink succeeded: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stderr, "is inside a cleanup target") {
		t.Fatalf("overlapping receipt sink stderr = %q; want the sink refusal", stderr)
	}
	if _, statErr := os.Stat(modules); statErr != nil {
		t.Fatalf("refused run deleted the cleanup target anyway: %v", statErr)
	}
	if _, statErr := os.Stat(receiptPath); !os.IsNotExist(statErr) {
		t.Fatalf("refused run wrote a receipt file: %v", statErr)
	}
}
