package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/executor"
)

func interactiveClean(ctx context.Context, input *confirminput.Reader, targets []preparedCleanTarget) (cleanExecutionReceipt, error) {
	return interactiveCleanWithValidation(ctx, input, targets, nil)
}

// interactiveCleanSkipOutcome reports a target left without mutation.
// Declined records an explicit refusal; otherwise the request was cancelled.
// AfterConfirmation identifies cancellation during validation after an approval.
// Observers are informational: they cannot affect cleanup safety, execution,
// or the printed confirmation.
type interactiveCleanSkipOutcome struct {
	Target            preparedCleanTarget
	Declined          bool
	AfterConfirmation bool
}

type interactiveCleanSkipObserver func(interactiveCleanSkipOutcome)

func interactiveCleanWithValidation(
	ctx context.Context,
	input *confirminput.Reader,
	targets []preparedCleanTarget,
	validate func(context.Context) error,
) (cleanExecutionReceipt, error) {
	return interactiveCleanWithValidationAndObserver(ctx, input, os.Stdout, targets, validate, nil)
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
	input *confirminput.Reader,
	output io.Writer,
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
	cancelRemaining := func(remaining []preparedCleanTarget, err error, afterConfirmation bool) (cleanExecutionReceipt, error) {
		for _, target := range remaining {
			if observer != nil {
				observer(interactiveCleanSkipOutcome{Target: target, AfterConfirmation: afterConfirmation})
			}
			unit := executor.CancelledPreparedCleanUnitReceipt(target.Item, target.Component, err, cleanJSONReceiptItemKey)
			unit.ResidualBytes = target.Item.Size
			result.Units = append(result.Units, unit)
		}
		return result, errors.Join(append(errs, err)...)
	}
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return cancelRemaining(targets[i:], err, false)
		}
		w := target.Item
		if !cleaner.IsSafeTarget(home, w) {
			err := fmt.Errorf("unsafe path %q rejected", w.Path)
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
			result.Units = append(result.Units, failedPreparedCleanUnitReceipt(target, err))
			errs = append(errs, err)
			continue
		}
		fmt.Fprintln(output)
		printCleanTargetTo(output, w, displayHome)
		fmt.Fprint(output, "Remove? [y/N]: ")
		line, ok, inputErr := confirminput.Scan(ctx, input)
		if inputErr != nil {
			if ctx.Err() != nil {
				return cancelRemaining(targets[i:], ctx.Err(), false)
			}
			reportUnansweredCleanTargets(observer, targets[i:])
			return result, errors.Join(append(errs, inputErr)...)
		}
		if !ok {
			reportUnansweredCleanTargets(observer, targets[i:])
			break
		}
		response := strings.TrimSpace(strings.ToLower(line))
		if response == "y" || response == "yes" {
			if validate != nil {
				if err := validate(ctx); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return cancelRemaining(targets[i:], err, true)
					}
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
			fmt.Fprintln(output, "  skipped")
			if observer != nil {
				observer(interactiveCleanSkipOutcome{Target: target, Declined: true})
			}
		}
	}
	return result, errors.Join(errs...)
}
