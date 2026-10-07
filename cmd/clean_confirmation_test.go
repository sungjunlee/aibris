package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

func TestCleanFinalConfirmationsInput(t *testing.T) {
	for _, prompt := range []struct {
		name string
		read func(context.Context, io.Reader, io.Writer) (bool, error)
	}{{"clean", confirmCleanExecution}, {"APFS", confirmAPFSSnapshotThin}} {
		for _, input := range []string{"y\n", "Y\n", "n\n", "", "maybe\n", "y extra\n"} {
			t.Run(prompt.name+"/"+input, func(t *testing.T) {
				approved, err := prompt.read(t.Context(), strings.NewReader(input), io.Discard)
				want := input == "y\n" || input == "Y\n"
				if err != nil || approved != want {
					t.Fatalf("confirmation = %t, %v; want %t", approved, err, want)
				}
			})
		}
	}
}

func TestCleanConfirmationsContextCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targets := confirmationTestTargets(t, home, 2)
	prompts := map[string]func(context.Context, io.Reader) error{
		"final": func(ctx context.Context, input io.Reader) error {
			_, err := confirmCleanExecution(ctx, input, io.Discard)
			return err
		},
		"APFS": func(ctx context.Context, input io.Reader) error {
			_, err := confirmAPFSSnapshotThin(ctx, input, io.Discard)
			return err
		},
		"guided": func(ctx context.Context, input io.Reader) error {
			_, _, err := promptGuidedClean(ctx, input, io.Discard, guidedCleanState{})
			return err
		},
		"unified": func(ctx context.Context, input io.Reader) error {
			_, _, err := promptUnifiedCleanupReview(ctx, input, io.Discard, UnifiedCleanupPlan{}, cleanupReviewText, 0)
			return err
		},
		"per-item": func(ctx context.Context, input io.Reader) error {
			receipt, err := interactiveCleanWithValidationAndObserver(ctx, input, io.Discard, targets, nil, nil)
			if len(receipt.Units) != len(targets) {
				return errors.New("pending cancellation units missing")
			}
			for _, unit := range receipt.Units {
				if unit.State != cleanExecutionCancelled || unit.PhysicalRemoved || unit.FreedBytes != 0 {
					return errors.New("pending target not cancelled")
				}
			}
			return err
		},
	}
	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			input, writer := io.Pipe()
			defer input.Close()
			defer writer.Close()
			started := make(chan struct{})
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- prompt(ctx, &confirmationBarrierReader{Reader: input, started: started}) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("prompt did not start reading")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("confirmation ignored context cancellation")
			}
			for _, target := range targets {
				if _, err := os.Stat(target.Item.Path); err != nil {
					t.Fatalf("cancelled confirmation mutated target: %v", err)
				}
			}
		})
	}
}

type confirmationBarrierReader struct {
	io.Reader
	started chan struct{}
}

func (r *confirmationBarrierReader) Read(p []byte) (int, error) {
	close(r.started)
	return r.Reader.Read(p)
}

type confirmationCancellingReader struct{ cancel context.CancelFunc }

func (r confirmationCancellingReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "y\n"), io.EOF
}

func TestCleanConfirmationInputDuringCancellationPreservesTargets(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targets := confirmationTestTargets(t, home, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	receipt, err := interactiveCleanWithValidationAndObserver(ctx, confirmationCancellingReader{cancel}, io.Discard, targets, nil, nil)
	if !errors.Is(err, context.Canceled) || len(receipt.Units) != len(targets) {
		t.Fatalf("receipt=%+v error=%v", receipt, err)
	}
	for _, target := range targets {
		if _, err := os.Stat(target.Item.Path); err != nil {
			t.Fatalf("input during cancellation mutated target: %v", err)
		}
	}
}

func TestInteractiveCleanConfirmationInput(t *testing.T) {
	for _, input := range []string{"y\n", "n\n", "", "invalid\n"} {
		t.Run(input, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			targets := confirmationTestTargets(t, home, 1)
			var outcomes []interactiveCleanSkipOutcome
			receipt, err := interactiveCleanWithValidationAndObserver(t.Context(), strings.NewReader(input), io.Discard, targets, nil, func(outcome interactiveCleanSkipOutcome) { outcomes = append(outcomes, outcome) })
			if err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(targets[0].Item.Path)
			if input == "y\n" {
				if !os.IsNotExist(statErr) || len(receipt.Units) != 1 || !receipt.Units[0].PhysicalRemoved {
					t.Fatalf("approval did not remove fixture: receipt=%+v stat=%v", receipt, statErr)
				}
			} else if statErr != nil || len(outcomes) != 1 || outcomes[0].Declined != (input != "") {
				t.Fatalf("decline/EOF receipt=%+v outcomes=%+v stat=%v", receipt, outcomes, statErr)
			}
		})
	}
}

func confirmationTestTargets(t *testing.T, home string, count int) []preparedCleanTarget {
	t.Helper()
	var items []types.DebrisInfo
	for i := 0; i < count; i++ {
		path := filepath.Join(home, string(rune('a'+i)), "node_modules")
		writeJSONReceiptFixture(t, path, "sentinel")
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, types.DebrisInfo{Path: path, Category: types.CategoryNodeModules, ModTime: info.ModTime()})
	}
	runtime := staticOverlapSafetyRuntime(nil, nil)
	selection, err := applyCleanupOverlapSafety(t.Context(), runtime, items)
	if err != nil {
		t.Fatal(err)
	}
	return prepareCleanExecutionWithSafety(t.Context(), selection, runtime)
}

func TestCleanReviewInput(t *testing.T) {
	for _, input := range []string{"", "\n", "y\n\n", "n\n\n", "invalid\n\n", "q\n"} {
		t.Run(input, func(t *testing.T) {
			_, aborted, err := promptGuidedClean(t.Context(), strings.NewReader(input), io.Discard, guidedCleanState{})
			if err != nil || aborted != (input == "q\n") {
				t.Fatalf("guided review = aborted %t, error %v", aborted, err)
			}
			_, aborted, err = promptUnifiedCleanupReview(t.Context(), strings.NewReader(input), io.Discard, UnifiedCleanupPlan{}, cleanupReviewText, 0)
			if err != nil || aborted != (input == "q\n") {
				t.Fatalf("unified review = aborted %t, error %v", aborted, err)
			}
		})
	}
}

func TestInteractiveCleanValidationCancellationDisposesPending(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	targets := confirmationTestTargets(t, home, 2)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var outcomes []interactiveCleanSkipOutcome
	receipt, err := interactiveCleanWithValidationAndObserver(ctx, strings.NewReader("y\n"), io.Discard, targets, func(context.Context) error { cancel(); return ctx.Err() }, func(outcome interactiveCleanSkipOutcome) { outcomes = append(outcomes, outcome) })
	if !errors.Is(err, context.Canceled) || len(receipt.Units) != len(targets) || len(outcomes) != len(targets) {
		t.Fatalf("receipt=%+v outcomes=%+v error=%v", receipt, outcomes, err)
	}
	for _, unit := range receipt.Units {
		if unit.State != cleanExecutionCancelled {
			t.Fatalf("pending unit = %+v", unit)
		}
		if _, err := os.Stat(unit.Target.Path); err != nil {
			t.Fatalf("cancelled validation mutated target: %v", err)
		}
	}
}

func TestAPFSSnapshotCancellationStopsNewPasses(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	previousList, previousThin, previousInspect := listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn = previousList, previousThin, previousInspect
	})
	remaining, passes := 2, 0
	listLocalAPFSSnapshots = func() (int, error) { return remaining, nil }
	thinLocalAPFSSnapshots = func() error { passes++; remaining--; cancel(); return nil }
	inspectHomeCapacityFn = func() (*volume.Report, error) { return nil, errors.New("fixture volume unavailable") }
	err := runAPFSSnapshotAction(ctx, false, true)
	if !errors.Is(err, context.Canceled) || passes != 1 {
		t.Fatalf("APFS cancellation = %v, passes %d; want cancelled after one pass", err, passes)
	}
}
