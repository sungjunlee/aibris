package cleaner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	errCleanupCommandNotFound = errors.New("cleanup command not found")
	lookPath                  = exec.LookPath
	commandContext            = exec.CommandContext
)

func runMutationBarrier(ctx context.Context, barrier MutationBarrier, item types.DebrisInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if barrier == nil {
		return nil
	}
	if err := barrier(ctx, item); err != nil {
		return fmt.Errorf("pre-mutation safety barrier for %q: %w", item.Path, err)
	}
	return ctx.Err()
}

func debrisName(w types.DebrisInfo) string {
	if w.ID != "" {
		return w.ID
	}
	return string(w.Tool)
}

func cleanupKind(w types.DebrisInfo) types.CleanupKind {
	if w.CleanupKind != "" {
		return w.CleanupKind
	}
	return types.CleanupRemovePath
}

func refuseStaleGoCache(item types.DebrisInfo) error {
	if !isGoCleanCache(item.CleanupCommand) {
		return nil
	}
	return adapter.RefuseStaleGoCache(item.Path)
}

func isGoCleanCache(argv []string) bool {
	return len(argv) == 3 && argv[0] == "go" && argv[1] == "clean" && argv[2] == "-cache"
}

func reportCommandCleaned(output io.Writer, w types.DebrisInfo, freed, residual int64) {
	if residual > 0 {
		fmt.Fprintf(output, "cleaned: %s (%s) via %s — %s remaining %s\n",
			w.ID, w.Tool, strings.Join(w.CleanupCommand, " "), FormatSize(freed), FormatSize(residual))
		return
	}
	fmt.Fprintf(output, "cleaned: %s (%s) via %s — %s\n",
		w.ID, w.Tool, strings.Join(w.CleanupCommand, " "), FormatSize(freed))
}

func reportCommandResidual(output io.Writer, w types.DebrisInfo, freed, residual int64) {
	if freed == 0 && residual == 0 {
		return
	}
	fmt.Fprintf(output, "failed: %s remaining %s (freed %s)\n",
		w.ID, FormatSize(residual), FormatSize(freed))
}

func runCleanupCommand(ctx context.Context, argv []string, beforeStart func()) error {
	if len(argv) == 0 {
		return nil
	}
	bin, err := lookPath(argv[0])
	if err != nil {
		return errCleanupCommandNotFound
	}
	cmd := commandContext(ctx, bin, argv[1:]...)
	if beforeStart != nil {
		beforeStart()
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(output) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return err
	}
	return nil
}
