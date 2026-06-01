package adapter

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

// DebrisProvider is implemented by each adapter to discover debris from a specific AI tool.
type DebrisProvider interface {
	Name() types.Tool
	Category() types.Category
	Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error)
}
