package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

func interactiveClean(ctx context.Context, targets []preparedCleanTarget) (cleanExecutionReceipt, error) {
	return interactiveCleanWithValidation(ctx, targets, nil)
}

// interactiveCleanSkipOutcome reports a prepared target the confirmation loop
// left without an execution unit. Declined is true when the operator answered
// the prompt and refused, and false when the confirmation stream ended before
// an answer arrived. Observers are informational: they cannot affect cleanup
// safety, execution, or the printed confirmation.
type interactiveCleanSkipOutcome struct {
	Target   preparedCleanTarget
	Declined bool
}

type interactiveCleanSkipObserver func(interactiveCleanSkipOutcome)

func interactiveCleanWithValidation(
	ctx context.Context,
	targets []preparedCleanTarget,
	validate func(context.Context) error,
) (cleanExecutionReceipt, error) {
	return interactiveCleanWithValidationAndObserver(ctx, targets, validate, nil)
}

// reportUnansweredCleanTargets hands the targets whose confirmation never
// arrived to an optional observer. It prints nothing, so the confirmation loop
// reads identically with and without an observer.
func reportUnansweredCleanTargets(
	observer interactiveCleanSkipObserver,
	targets []preparedCleanTarget,
) {
	if observer == nil {
		return
	}
	for _, target := range targets {
		observer(interactiveCleanSkipOutcome{Target: target})
	}
}

func interactiveCleanWithValidationAndObserver(
	ctx context.Context,
	targets []preparedCleanTarget,
	validate func(context.Context) error,
	observer interactiveCleanSkipObserver,
) (cleanExecutionReceipt, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return cleanExecutionReceipt{}, fmt.Errorf("getting home dir: %w", err)
	}
	displayHome := resolvedDisplayHome(home)

	var result cleanExecutionReceipt
	var errs []error
	scanner := bufio.NewScanner(os.Stdin)
	for i, target := range targets {
		w := target.Item
		if !cleaner.IsSafeTarget(home, w) {
			err := fmt.Errorf("unsafe path %q rejected", w.Path)
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
			result.Units = append(result.Units, failedPreparedCleanUnitReceipt(target, err))
			errs = append(errs, err)
			continue
		}
		fmt.Println()
		printCleanTarget(w, displayHome)
		fmt.Print("Remove? [y/N]: ")
		if !scanner.Scan() {
			reportUnansweredCleanTargets(observer, targets[i:])
			break
		}
		response := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if response == "y" || response == "yes" {
			if validate != nil {
				if err := validate(ctx); err != nil {
					for _, remaining := range targets[i:] {
						result.Units = append(result.Units, failedPreparedCleanUnitReceipt(remaining, err))
					}
					return result, err
				}
			}
			receipt, err := executePreparedCleanTargets(ctx, []preparedCleanTarget{target}, defaultActiveWorktreeExecutionOptions())
			result.Units = append(result.Units, receipt.Units...)
			result.FreedBytes += receipt.FreedBytes
			if err != nil {
				fmt.Fprintf(os.Stderr, "  error: %v\n", err)
				errs = append(errs, err)
				continue
			}
		} else {
			fmt.Printf("  skipped\n")
			if observer != nil {
				observer(interactiveCleanSkipOutcome{Target: target, Declined: true})
			}
		}
	}
	return result, errors.Join(errs...)
}
