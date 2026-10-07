package adapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/sungjunlee/aibris/internal/types"
)

// ErrCleanupRecipeChanged marks an inventory recipe with no live authority.
var ErrCleanupRecipeChanged = errors.New("cleanup recipe no longer matches live catalog")

// ResolveCleanupCommand treats argv as an inventory claim, never executable
// authority. Both argv and env come from the live catalog after matching the
// tool, category and canonical target. Removed recipes fail closed; callers
// must not turn this refusal into path-removal fallback.
func ResolveCleanupCommand(item types.DebrisInfo) (argv, env []string, err error) {
	refuse := func() ([]string, []string, error) {
		return nil, nil, fmt.Errorf("%w for %q", ErrCleanupRecipeChanged, item.Path)
	}
	if item.CleanupKind != types.CleanupCommand || len(item.CleanupCommand) == 0 || !filepath.IsAbs(item.Path) {
		return refuse()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return refuse()
	}
	canonical, err := filepath.EvalSymlinks(item.Path)
	if err != nil {
		return refuse()
	}
	for _, target := range cacheCatalog {
		category := types.CategoryBuildCache
		if target.tool == types.ToolPipCache {
			category = types.CategoryOtherCache
		}
		if target.tool != item.Tool || category != item.Category || len(target.command) == 0 || target.commandEnv == nil {
			continue
		}
		path := target.resolve(home)
		if path == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || filepath.Clean(resolved) != filepath.Clean(canonical) {
			continue
		}
		command := target.command
		if !slices.Equal(item.CleanupCommand, command) {
			if len(target.pressureCommand) == 0 || !slices.Equal(item.CleanupCommand, target.pressureCommand) {
				return refuse()
			}
			command = target.pressureCommand
		}
		env := target.commandEnv(canonical)
		if len(env) == 0 {
			return refuse()
		}
		return slices.Clone(command), env, nil
	}
	return refuse()
}
