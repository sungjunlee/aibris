package cleaner

import (
	"context"

	"github.com/sungjunlee/aibris/internal/adapter"
)

func observedSize(ctx context.Context, path string) int64 {
	return adapter.EstimateDirSize(ctx, path)
}

func reclaimedBytes(before, after int64) int64 {
	if after > before {
		return 0
	}
	return before - after
}

func observeReclamation(ctx context.Context, path string, mutate func() error) (int64, int64, error) {
	measure := measureAfterContext(ctx)
	before := observedSize(measure, path)
	err := mutate()
	after := observedSize(measureAfterContext(ctx), path)
	return reclaimedBytes(before, after), after, err
}

func measureAfterContext(ctx context.Context) context.Context {
	if ctx.Err() != nil {
		return context.Background()
	}
	return ctx
}
