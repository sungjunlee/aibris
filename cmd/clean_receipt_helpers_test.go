package cmd

import (
	"errors"
	"io"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
)

// finalizeCleanJSONReceipt finalizes a receipt through the production
// FinishCleanJSONReceipt with the command's snapshot lister.
func finalizeCleanJSONReceipt(receipt cleanJSONReceipt) (cleanJSONReceipt, error) {
	return cleanjson.FinishCleanJSONReceipt(receipt, nil, listLocalAPFSSnapshots, func(err error) bool {
		return errors.Is(err, cleaner.ErrCleanupTargetYoungerThanMinimumAge)
	})
}

func quietActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	opts := defaultActiveWorktreeExecutionOptions()
	opts.Output = io.Discard
	opts.ErrorOutput = io.Discard
	return opts
}
