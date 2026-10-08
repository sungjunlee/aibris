package executor

import (
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// TargetIdentityKey identifies a logical target from its stable fields and
// canonical path. Capture it during preparation; a missing path may resolve
// differently, so execution must carry the captured key without recomputing it.
func TargetIdentityKey(item types.DebrisInfo) string {
	pathKey := strings.TrimSpace(item.Path)
	if canonical, ok := cleaner.TargetPathKey(item.Path); ok {
		pathKey = canonical
	} else if pathKey != "" {
		pathKey = cleaner.TargetRawPathKey(pathKey)
	} else {
		pathKey = "<empty-path>"
	}
	return strings.Join([]string{
		string(item.Category),
		string(item.Tool),
		item.ID,
		pathKey,
	}, "\x00")
}
