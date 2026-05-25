package adapter

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

// WorktreeProvider is implemented by each adapter to discover debris from a specific AI tool.
type WorktreeProvider interface {
	Name() types.Tool
	Category() types.Category
	Scan(ctx context.Context) ([]types.WorktreeInfo, error)
}
