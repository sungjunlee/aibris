package cleaner

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
)

var observedSize = adapter.EstimateDirSizeWithError
var observedResidualSize = adapter.EstimateDirSizeWithError

// Residual reporting must remain bounded even after a partially completed
// mutation cancels the execution context.
const residualMeasurementTimeout = 100 * time.Millisecond

func reclaimedBytes(before, after int64) int64 {
	if after > before {
		return 0
	}
	return before - after
}

// mutate contains the final barrier, after the report-only size measurement.
// Its bool distinguishes a refusal from a mutation attempt so cancellation
// before mutation neither claims reclaimed bytes nor walks the residual tree.
func observeReclamation(ctx context.Context, path string, mutate func() (bool, error)) (int64, int64, bool, error) {
	before, beforeErr := observedSize(ctx, path)
	if err := ctx.Err(); err != nil {
		return 0, before, false, err
	}
	attempted, err := mutate()
	// Cancellation cannot turn a successfully completed mutation into a
	// partial failure. It still bounds residual measurement below.
	if !attempted || err != nil {
		err = errors.Join(err, ctx.Err())
	}
	if !attempted {
		return 0, before, false, err
	}
	measure := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		measure, cancel = context.WithTimeout(context.WithoutCancel(ctx), residualMeasurementTimeout)
		defer cancel()
	}
	after, measureErr := observedResidualSize(measure, path)
	if os.IsNotExist(measureErr) {
		// Only disappearance of the target proves a zero residual. A missing
		// descendant during a walk is still incomplete evidence.
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			after, measureErr = 0, nil
		}
	}
	measurementInterrupted := measure.Err() != nil || errors.Is(measureErr, context.Canceled) || errors.Is(measureErr, context.DeadlineExceeded)
	if measureErr != nil && after < before && (beforeErr == nil || measurementInterrupted) {
		// A complete baseline cannot be compared with an incomplete residual;
		// a canceled walk may also omit readable bytes counted before mutation.
		// Otherwise retain approximate reporting when both walks are incomplete
		// (for example, the same unreadable sibling survives a partial removal).
		after = before
	}
	return reclaimedBytes(before, after), after, true, err
}
