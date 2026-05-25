package adapter

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

type WorktreeProvider interface {
	Name() types.Tool
	Scan(ctx context.Context) ([]types.WorktreeInfo, error)
}
